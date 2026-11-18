package fn

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFaviconCannotDiscoverOrRedirectToAnotherPrivateService(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("NO_PROXY", "*")
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	}))
	defer target.Close()
	for _, mode := range []string{"redirect", "discovery"} {
		t.Run(mode, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, target.URL+"/private", http.StatusFound)
					return
				}
				if r.URL.Path == "/favicon.ico" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, `<html><head><link rel="icon" href="`+strings.Replace(target.URL, "127.0.0.1", "localhost", 1)+`/private"></head></html>`)
			}))
			defer origin.Close()
			_, _, err := FetchPublicSiteFavicon(origin.URL + "/favicon.ico")
			if err == nil {
				t.Fatal("unconfigured private destination accepted")
			}
			if hits.Load() != 0 {
				t.Fatal("request escaped to a different private service")
			}
		})
	}
}

func TestFaviconRejectsPrivateDNSForPublicHostname(t *testing.T) {
	ctx := withSiteIconOrigin(context.Background(), "https://public.example/favicon.ico")
	u, err := url.Parse("https://public.example/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolveSiteIconAddressesWithLookup(ctx, u, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.168.1.5")}, nil
	})
	if err == nil {
		t.Fatal("public hostname resolving to a private address should be rejected")
	}
}

func TestFaviconAllowsPrivateDNSForLocalHostname(t *testing.T) {
	ctx := withSiteIconOrigin(context.Background(), "http://nas.local/favicon.ico")
	u, err := url.Parse("http://nas.local/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	addresses, err := resolveSiteIconAddressesWithLookup(ctx, u, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.168.1.5")}, nil
	})
	if err != nil || len(addresses) != 1 || addresses[0] != "192.168.1.5:80" {
		t.Fatalf("local favicon addresses = %v, err = %v", addresses, err)
	}
}

func TestFaviconRejectsPrivateAddressOnPublicRedirect(t *testing.T) {
	ctx := withSiteIconOrigin(context.Background(), "https://public.example/favicon.ico")
	u, err := url.Parse("https://internal.example/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolveSiteIconAddressesWithLookup(ctx, u, func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.7")}, nil
	})
	if err == nil {
		t.Fatal("public redirect host resolving to a private address should be rejected")
	}
}

func TestConfiguredLocalFaviconCanRedirectWithinSameService(t *testing.T) {
	t.Chdir(t.TempDir())
	const svg = `<svg xmlns="http://www.w3.org/2000/svg"/>`
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			http.Redirect(w, r, "/icons/site.svg", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, svg)
	}))
	defer origin.Close()
	raw, _, err := FetchPublicSiteFavicon(origin.URL + "/favicon.ico")
	if err != nil || string(raw) != svg {
		t.Fatalf("configured local icon: %v, %q", err, raw)
	}
}
