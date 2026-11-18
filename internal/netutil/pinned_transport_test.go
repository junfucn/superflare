package netutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junfuchang/superflare/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestPinnedTransportKeepsValidatedIPAndOriginalHost(t *testing.T) {
	for _, useProxy := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "proxy"}[useProxy], func(t *testing.T) {
			var originHits, proxyHits, resolutions atomic.Int32
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originHits.Add(1)
				if r.Host != "public.example.com" || r.TLS.ServerName != "public.example.com" || r.Header.Get("Proxy-Authorization") != "" {
					t.Error("original Host/TLS name changed or proxy credentials leaked")
				}
				if r.URL.Path == "/redirect" {
					http.Redirect(w, r, "/final", http.StatusFound)
					return
				}
				_, _ = io.WriteString(w, "image")
			}))
			defer origin.Close()
			proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyHits.Add(1)
				if r.Method != http.MethodConnect || r.Host != "93.184.216.55:443" || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
					t.Errorf("proxy received an unpinned request: %s %s", r.Method, r.Host)
					http.Error(w, "invalid CONNECT", http.StatusBadRequest)
					return
				}
				upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
				if err != nil {
					t.Error(err)
					http.Error(w, "dial failed", http.StatusBadGateway)
					return
				}
				defer upstream.Close()
				conn, buffered, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				_ = buffered.Flush()
				done := make(chan struct{})
				go func() { _, _ = io.Copy(upstream, buffered); close(done) }()
				_, _ = io.Copy(conn, upstream)
				_ = conn.Close()
				<-done
			}))
			defer proxyServer.Close()
			proxyURL, err := url.Parse(proxyServer.URL)
			require.NoError(t, err)
			proxyURL.User = url.UserPassword("user", "pass")
			denied := errors.New("destination denied")
			var denyNext atomic.Bool
			transport := NewPinnedTransport(2*time.Second, func(ctx context.Context, u *url.URL) ([]string, error) {
				resolutions.Add(1)
				if denyNext.Load() || u.Path == "/final" {
					return nil, denied
				}
				if useProxy {
					return []string{"93.184.216.55:443"}, nil
				}
				return []string{origin.Listener.Addr().String()}, nil
			}, func(*http.Request) (*url.URL, error) {
				if useProxy {
					return proxyURL, nil
				}
				return nil, nil
			})
			roots := x509.NewCertPool()
			roots.AddCert(origin.Certificate())
			transport.Transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			defer client.CloseIdleConnections()
			response, err := client.Get("https://public.example.com/image")
			require.NoError(t, err)
			raw, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			require.NoError(t, err)
			require.Equal(t, "image", string(raw))
			_, err = client.Get("https://public.example.com/redirect")
			require.ErrorIs(t, err, denied)
			denyNext.Store(true)
			_, err = client.Get("https://public.example.com/image")
			require.ErrorIs(t, err, denied)
			require.Equal(t, int32(4), resolutions.Load(), "all requests and redirects must revalidate")
			require.Equal(t, int32(2), originHits.Load(), "denied requests must never reach the origin")
			if useProxy {
				require.Equal(t, int32(2), proxyHits.Load(), "denied requests must never reach the proxy")
			}
		})
	}
}

func TestPinnedTransportTunnelsHTTPWithOriginalHost(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[useTLS], func(t *testing.T) {
			var requests, originHits atomic.Int32
			bodyReady := make(chan struct{})
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originHits.Add(1)
				if r.Host != "public.example.com" || r.RequestURI != "/image%2Ficon?q=1" {
					t.Errorf("origin request changed: Host=%q URI=%q", r.Host, r.RequestURI)
				}
				if r.Header.Get("Proxy-Authorization") != "" {
					t.Error("proxy credentials leaked to origin")
				}
				w.Header().Set("Content-Length", "2")
				w.(http.Flusher).Flush()
				select {
				case <-bodyReady:
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "ok")
			}))
			defer origin.Close()
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodConnect || r.Host != "93.184.216.88:80" {
					t.Errorf("proxy received unpinned request: %s %s", r.Method, r.Host)
					http.Error(w, "invalid CONNECT", http.StatusBadRequest)
					return
				}
				if r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
					t.Errorf("proxy authorization = %q", r.Header.Get("Proxy-Authorization"))
				}
				testutil.ForwardProxyTunnel(t, w, r, origin.Listener.Addr().String())
			})
			var proxyServer *httptest.Server
			if useTLS {
				proxyServer = httptest.NewTLSServer(handler)
			} else {
				proxyServer = httptest.NewServer(handler)
			}
			defer proxyServer.Close()
			proxyURL, err := url.Parse(proxyServer.URL)
			require.NoError(t, err)
			proxyURL.User = url.UserPassword("user", "pass")
			transport := NewPinnedTransport(2*time.Second, func(context.Context, *url.URL) ([]string, error) {
				return []string{"93.184.216.88:80"}, nil
			}, func(*http.Request) (*url.URL, error) { return proxyURL, nil })
			if useTLS {
				roots := x509.NewCertPool()
				roots.AddCert(proxyServer.Certificate())
				transport.Transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			}
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			defer client.CloseIdleConnections()

			request, err := http.NewRequest(http.MethodGet, "http://public.example.com/image%2Ficon?q=1", nil)
			require.NoError(t, err)
			response, err := client.Do(request)
			close(bodyReady)
			require.NoError(t, err)
			raw, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			require.NoError(t, err)
			require.Equal(t, "ok", string(raw))
			require.Equal(t, request.URL.String(), response.Request.URL.String(), "redirects must retain the original hostname")
			require.Equal(t, int32(1), requests.Load())
			require.Equal(t, int32(1), originHits.Load())
		})
	}
}

func TestPinnedTransportFallsBackAfterHTTPProxyTimeout(t *testing.T) {
	const first = "[2001:4860:4860::8888]:80"
	const second = "93.184.216.35:80"
	attempts := make(chan string, 2)
	firstClosed := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "public.example.com" || r.RequestURI != "/image" {
			t.Errorf("unexpected origin request: Host=%q URI=%q", r.Host, r.RequestURI)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer origin.Close()
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts <- r.RequestURI
		if r.Method != http.MethodConnect {
			t.Errorf("expected CONNECT, got %s", r.Method)
			http.Error(w, "invalid CONNECT", http.StatusBadRequest)
			return
		}
		if r.Host == first {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			_, _ = io.Copy(io.Discard, conn)
			close(firstClosed)
			return
		}
		if r.RequestURI != second || r.Host != second {
			t.Errorf("unexpected pinned proxy request: %s %s Host=%s", r.Method, r.RequestURI, r.Host)
			return
		}
		testutil.ForwardProxyTunnel(t, w, r, origin.Listener.Addr().String())
	}))
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	transport := NewPinnedTransport(time.Second, func(context.Context, *url.URL) ([]string, error) {
		return []string{first, second}, nil
	}, func(*http.Request) (*url.URL, error) { return proxyURL, nil })
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://public.example.com/image")
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(raw))
	require.Len(t, attempts, 2)
	require.Equal(t, first, <-attempts)
	require.Equal(t, second, <-attempts)
	select {
	case <-firstClosed:
	case <-time.After(time.Second):
		t.Fatal("timed-out proxy connection was not closed")
	}
}

func TestDialPinnedContextStopsOnCancellation(t *testing.T) {
	var attempts atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		cancel()
		<-r.Context().Done()
	}))
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	conn, err := DialPinnedContext(ctx, time.Second, "tcp", []string{"93.184.216.34:80", "93.184.216.35:80"}, proxyURL)
	if conn != nil {
		_ = conn.Close()
	}
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int32(1), attempts.Load())
}
