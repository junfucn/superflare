package editor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLinkCheckRejectsNonPublicAddresses(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "169.254.169.254",
		"0.0.0.0", "0.1.2.3", "100.100.100.200", "198.18.0.1", "224.0.0.1", "240.0.0.1",
		"192.0.2.1", "198.51.100.1", "203.0.113.1",
		"::", "::1", "fc00::1", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1",
		"64:ff9b::7f00:1", "64:ff9b:1::1", "2002:7f00:1::1", "2001::1",
		"::127.0.0.1", "fec0::1", "2001:db8::1", "3fff::1",
	} {
		require.False(t, isPublicLinkCheckIP(netip.MustParseAddr(address)), address)
	}
	for _, address := range []string{"8.8.8.8", "93.184.215.14", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		require.True(t, isPublicLinkCheckIP(netip.MustParseAddr(address)), address)
	}
	for _, raw := range []string{"http://localhost/", "http://localhost./", "http://nas.local/", "http://user:pass@example.com/", "file://example.com/"} {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		require.ErrorIs(t, validateLinkCheckURL(u), errLinkCheckDestinationNotAllowed, raw)
	}
}

func TestLinkCheckRejectsPrivateDNSBeforeDialingOrProxying(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("10.0.0.1")},
		{netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("::1")},
	} {
		transport := newLinkCheckTransport()
		transport.lookup = func(context.Context, string, string) ([]netip.Addr, error) { return addresses, nil }
		transport.proxy = func(*http.Request) (*url.URL, error) {
			t.Error("private destination reached proxy selection")
			return nil, nil
		}
		transport.transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
			t.Error("private destination reached dialer")
			return nil, io.ErrUnexpectedEOF
		}
		req, err := http.NewRequest(http.MethodGet, "http://public.example.com/", nil)
		require.NoError(t, err)
		_, err = transport.RoundTrip(req)
		require.ErrorIs(t, err, errLinkCheckDestinationNotAllowed)
	}
}

func TestLinkCheckRedirectsCannotReachLocalService(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	for _, destination := range []string{target.URL, "http://private.example.com/", "http://[::ffff:127.0.0.1]/"} {
		t.Run(destination, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination, http.StatusFound)
			}))
			defer origin.Close()
			client := newLinkCheckClient()
			defer client.CloseIdleConnections()
			transport := client.Transport.(*linkCheckTransport)
			transport.lookup = func(ctx context.Context, network, host string) ([]netip.Addr, error) {
				if host == "private.example.com" {
					return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
				}
				return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
			}
			transport.proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
			var dials atomic.Int32
			transport.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				if address == "public.example.com:80" {
					return (&net.Dialer{}).DialContext(ctx, network, origin.Listener.Addr().String())
				}
				return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
			}
			result := checkOneLink(context.Background(), client, linkCheckItem{Row: 1, URL: "http://public.example.com/"})
			require.Equal(t, "restricted", result.Status)
			require.Equal(t, int32(1), dials.Load(), "blocked requests should not be retried or dialed")
			require.Zero(t, hits.Load())
		})
	}
}

func TestLinkCheckDirectConnectionPinsDNSAndPreservesTLSHostname(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Host != "public.example.com" || r.TLS.ServerName != "public.example.com" {
			t.Error("original HTTP and TLS hostname was lost")
		}
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	client := newLinkCheckClient()
	defer client.CloseIdleConnections()
	transport := client.Transport.(*linkCheckTransport)
	var lookups atomic.Int32
	transport.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		lookups.Add(1)
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}
	transport.proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	transport.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		target := ctx.Value(linkCheckDialKey{}).(linkCheckDialTarget)
		if len(target.addresses) != 1 || target.addresses[0] != "93.184.215.14:443" || target.proxyURL != nil {
			t.Errorf("connection was not pinned to validated public IP: %+v", target)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	result := checkOneLink(context.Background(), client, linkCheckItem{Row: 1, URL: "https://public.example.com/"})
	require.Equal(t, "ok", result.Status, result.Reason)
	require.Equal(t, int32(2), lookups.Load(), "each redirect must resolve once")
	require.Equal(t, int32(2), requests.Load())

	transport.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	result = checkOneLink(context.Background(), client, linkCheckItem{Row: 1, URL: "https://public.example.com/final"})
	require.Equal(t, "restricted", result.Status)
	require.Equal(t, int32(2), requests.Load(), "rebound DNS must not reach the server")
}

func TestLinkCheckProxyTunnelsOnlyValidatedIPs(t *testing.T) {
	for _, useTLS := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[useTLS], func(t *testing.T) {
			var originRequests atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originRequests.Add(1)
				if r.Host != "public.example.com" || r.Header.Get("Proxy-Authorization") != "" {
					t.Error("proxy leaked credentials or changed the origin Host")
				}
				_, _ = io.WriteString(w, "ok")
			})
			var origin *httptest.Server
			scheme, port := "http", "80"
			if useTLS {
				origin = httptest.NewTLSServer(handler)
				scheme, port = "https", "443"
			} else {
				origin = httptest.NewServer(handler)
			}
			defer origin.Close()
			var proxyRequests atomic.Int32
			proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyRequests.Add(1)
				if r.Method != http.MethodConnect || r.Host != "93.184.215.14:"+port || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
					t.Errorf("unexpected proxy request: %s %s", r.Method, r.Host)
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
			proxyURL := strings.Replace(proxyServer.URL, "://", "://user:pass@", 1)
			t.Setenv("HTTP_PROXY", proxyURL)
			t.Setenv("HTTPS_PROXY", proxyURL)
			t.Setenv("NO_PROXY", "")
			t.Setenv("no_proxy", "")
			client := newLinkCheckClient()
			defer client.CloseIdleConnections()
			transport := client.Transport.(*linkCheckTransport)
			transport.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
			}
			if useTLS {
				roots := x509.NewCertPool()
				roots.AddCert(origin.Certificate())
				transport.transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			}
			result := checkOneLink(context.Background(), client, linkCheckItem{Row: 1, URL: scheme + "://public.example.com/"})
			require.Equal(t, "ok", result.Status, result.Reason)
			require.Equal(t, int32(1), proxyRequests.Load())
			require.Equal(t, int32(1), originRequests.Load())
			transport.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
			}
			result = checkOneLink(context.Background(), client, linkCheckItem{Row: 2, URL: scheme + "://private.example.com/"})
			require.Equal(t, "restricted", result.Status)
			require.Equal(t, int32(1), proxyRequests.Load())
		})
	}
}

func TestLinkCheckFallsBackAfterProxyConnectTimeout(t *testing.T) {
	attempts := make(chan string, 2)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts <- r.Host
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if r.Host == "93.184.216.34:80" {
			_, _ = io.Copy(io.Discard, conn)
			return
		}
		if r.Method != http.MethodConnect || r.Host != "93.184.216.35:80" {
			t.Errorf("unexpected proxy destination: %s %s", r.Method, r.Host)
			return
		}
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		if _, err := http.ReadRequest(rw.Reader); err != nil {
			t.Error(err)
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
	}))
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	client := newLinkCheckClient()
	defer client.CloseIdleConnections()
	transport := client.Transport.(*linkCheckTransport)
	transport.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("93.184.216.35")}, nil
	}
	transport.proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	result := checkOneLinkOnce(context.Background(), client, linkCheckItem{Row: 1, URL: "http://public.example.com/"})
	require.Equal(t, "ok", result.Status, result.Reason)
	require.Len(t, attempts, 2)
	require.Equal(t, "93.184.216.34:80", <-attempts)
	require.Equal(t, "93.184.216.35:80", <-attempts)
}
