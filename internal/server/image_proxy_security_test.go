package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/background"
	"github.com/junfuchang/superflare/internal/fn"
	"github.com/junfuchang/superflare/internal/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func TestImageProxyPinsValidatedDNSAndRejectsReboundRedirect(t *testing.T) {
	var rebound atomic.Bool
	useImageProxyTestDNS(t, &rebound)
	for _, kind := range []string{"background", "favicon"} {
		for _, redirect := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/redirect=%t", kind, redirect), func(t *testing.T) {
				t.Chdir(t.TempDir())
				rebound.Store(false)
				var proxyRequests atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Host != "public-image.test" {
						t.Errorf("origin Host changed: %s", r.Host)
					}
					if redirect {
						rebound.Store(true)
						http.Redirect(w, r, "http://public-image.test/private.svg", http.StatusFound)
						return
					}
					const svg = `<svg xmlns="http://www.w3.org/2000/svg"/>`
					w.Header().Set("Content-Type", "image/svg+xml")
					_, _ = io.WriteString(w, svg)
				}))
				defer upstream.Close()
				proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxyRequests.Add(1)
					if r.Host != "93.184.216.34:80" {
						t.Errorf("proxy received an unpinned destination: %s %s", r.Method, r.Host)
						http.Error(w, "unpinned destination", http.StatusBadGateway)
						return
					}
					testutil.ForwardProxyTunnel(t, w, r, upstream.Listener.Addr().String())
				}))
				defer proxyServer.Close()
				t.Setenv("HTTP_PROXY", proxyServer.URL)
				t.Setenv("HTTPS_PROXY", proxyServer.URL)
				t.Setenv("NO_PROXY", "")
				t.Setenv("no_proxy", "")
				source := "http://public-image.test/image.svg"
				var raw []byte
				var err error
				if kind == "background" {
					raw, _, err = background.FetchRemoteVariant(source, "full")
				} else {
					// An operator-approved local source discovers a different host.
					// Only that original service may use a private address.
					origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						http.Redirect(w, r, source, http.StatusFound)
					}))
					defer origin.Close()
					raw, _, err = fn.FetchPublicSiteFavicon(origin.URL + "/image.svg")
				}
				if redirect {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.Contains(t, string(raw), "<svg")
				}
				require.Equal(t, int32(1), proxyRequests.Load(), "rebound DNS must not reach the proxy")
			})
		}
	}
}

func TestBackgroundRedirectRejectsSharedAddressBeforeProxyRequest(t *testing.T) {
	withRouterTestWorkingDir(t)
	var hits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "93.184.216.34" {
			t.Errorf("origin Host changed: %s", r.Host)
		}
		http.Redirect(w, r, "http://100.100.100.200/private.svg", http.StatusFound)
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Host != "93.184.216.34:80" {
			t.Errorf("unapproved destination reached proxy: %s %s", r.Method, r.Host)
			http.Error(w, "unexpected destination", http.StatusBadGateway)
			return
		}
		testutil.ForwardProxyTunnel(t, w, r, origin.Listener.Addr().String())
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	flags := newTestFlags(false, "DEFAULT", true)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)
	const source = "http://93.184.216.34/review-background.svg"
	require.NoError(t, data.UpdateThemeAndBackgroundSettings(model.Application{
		Theme: "blackboard", BackgroundImage: source, BackgroundImageMode: "url", BackgroundOpacity: 100,
	}))
	response := securityRequest(handler, http.MethodGet, "/assets/background-image?src="+url.QueryEscape(source), "", nil)
	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Equal(t, int32(1), hits.Load(), "shared-space redirect must not reach proxy")
}

func TestConfiguredFaviconKeepsOriginalIPsAcrossDNSRebinding(t *testing.T) {
	var rebound atomic.Bool
	useImageProxyTestDNS(t, &rebound)
	for _, mode := range []string{"redirect", "discovery"} {
		t.Run(mode, func(t *testing.T) {
			withRouterTestWorkingDir(t)
			rebound.Store(false)
			var proxyRequests atomic.Int32
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "public-image.test" {
					t.Errorf("origin Host changed: %s", r.Host)
				}
				switch r.URL.Path {
				case "/favicon.ico":
					rebound.Store(true)
					if mode == "redirect" {
						http.Redirect(w, r, "/icon.svg", http.StatusFound)
					} else {
						http.NotFound(w, r)
					}
				case "/":
					const page = `<html><head><link rel="icon" href="/icon.svg"></head></html>`
					w.Header().Set("Content-Type", "text/html")
					_, _ = io.WriteString(w, page)
				case "/icon.svg":
					const svg = `<svg xmlns="http://www.w3.org/2000/svg"><title>original service</title></svg>`
					w.Header().Set("Content-Type", "image/svg+xml")
					_, _ = io.WriteString(w, svg)
				default:
					t.Errorf("unexpected favicon path: %s", r.URL.Path)
				}
			}))
			defer origin.Close()
			proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyRequests.Add(1)
				if r.Host != "93.184.216.34:80" {
					t.Errorf("configured favicon escaped its original IP: %s %s", r.Method, r.Host)
					http.Error(w, "unexpected destination", http.StatusBadGateway)
					return
				}
				testutil.ForwardProxyTunnel(t, w, r, origin.Listener.Addr().String())
			}))
			defer proxyServer.Close()
			t.Setenv("HTTP_PROXY", proxyServer.URL)
			t.Setenv("HTTPS_PROXY", proxyServer.URL)
			t.Setenv("NO_PROXY", "")
			t.Setenv("no_proxy", "")
			flags := newTestFlags(false, "DEFAULT", true)
			h, err := NewRouter(&flags)
			require.NoError(t, err)
			require.NoError(t, data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{{Name: "Public", URL: "http://public-image.test/"}}}))
			rec := securityRequest(h, http.MethodGet, "/assets/site-icons?src="+url.QueryEscape("http://public-image.test/favicon.ico"), "", nil)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), "original service")
			require.True(t, rebound.Load())
			wantRequests := int32(2)
			if mode == "discovery" {
				wantRequests = 3
			}
			require.Equal(t, wantRequests, proxyRequests.Load())
		})
	}
}

func useImageProxyTestDNS(t *testing.T, rebound *atomic.Bool) {
	t.Helper()
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 2048)
		for {
			n, peer, err := server.ReadFrom(buffer)
			if err != nil {
				return
			}
			var parser dnsmessage.Parser
			header, err := parser.Start(buffer[:n])
			if err != nil {
				continue
			}
			question, err := parser.Question()
			if err != nil {
				continue
			}
			builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true, RecursionDesired: true, RecursionAvailable: true})
			_ = builder.StartQuestions()
			_ = builder.Question(question)
			_ = builder.StartAnswers()
			if question.Type == dnsmessage.TypeA && strings.TrimSuffix(question.Name.String(), ".") == "public-image.test" {
				ip := [4]byte{93, 184, 216, 34}
				if rebound.Load() {
					ip = [4]byte{127, 0, 0, 1}
				}
				_ = builder.AResource(dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, dnsmessage.AResource{A: ip})
			}
			response, err := builder.Finish()
			if err == nil {
				_, _ = server.WriteTo(response, peer)
			}
		}
	}()
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", server.LocalAddr().String())
	}}
	t.Cleanup(func() {
		net.DefaultResolver = previous
		_ = server.Close()
		<-done
	})
}
