package fn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchPublicSiteFaviconDiscoversSPAPublicConfigIcon(t *testing.T) {
	withSiteIconTempWorkingDir(t)
	icon := encodeTestFavicon(t, "png")
	var configRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/favicon.ico":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><script type="module" src="/assets/index-app.js"></script></head><body><div id="app"></div></body></html>`))
		case "/api/v1/public/config":
			configRequests.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"brand": map[string]string{"site_icon": "/uploads/site.png"}},
			})
		case "/uploads/site.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(icon)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	for attempt := 0; attempt < 2; attempt++ {
		data, contentType, err := FetchPublicSiteFavicon(upstream.URL + "/favicon.ico")
		if err != nil || contentType != "image/png" || !bytes.Equal(data, icon) {
			t.Fatalf("SPA favicon attempt %d: type=%q err=%v", attempt, contentType, err)
		}
	}
	if got := configRequests.Load(); got != 1 {
		t.Fatalf("public config requests = %d, want one discovery followed by a cache hit", got)
	}
}

func TestFetchPublicSiteFaviconTriesOtherDeclaredIconsWithHTMLBase(t *testing.T) {
	withSiteIconTempWorkingDir(t)
	const icon = `<svg xmlns="http://www.w3.org/2000/svg"><title>second-icon</title></svg>`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><base href="/assets/"><link rel="icon" href="missing.svg"><link rel="icon" href="real.svg"></head></html>`))
		case "/assets/real.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(icon))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	data, contentType, err := FetchPublicSiteFavicon(upstream.URL + "/favicon.ico")
	if err != nil || contentType != "image/svg+xml" || string(data) != icon {
		t.Fatalf("second declared favicon: type=%q err=%v data=%q", contentType, err, data)
	}
}

func TestCanceledSiteFaviconFetchDoesNotSuppressRetry(t *testing.T) {
	withSiteIconTempWorkingDir(t)
	oldClient := siteIconHTTPClient
	defer func() { siteIconHTTPClient = oldClient }()
	started := make(chan struct{})
	siteIconHTTPClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const source = "https://cancelled-fetch.test/favicon.ico"
	result := make(chan error, 1)
	go func() {
		_, _, err := FetchPublicSiteFaviconContext(ctx, source)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("favicon fetch did not start")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fetch error = %v", err)
	}
	if siteFaviconFailureActive(source) {
		t.Fatal("a disconnected browser must not put the icon into failure cooldown")
	}
}

func TestValidatedCachedSiteFaviconDoesNotWaitForDecodeSlots(t *testing.T) {
	withSiteIconTempWorkingDir(t)
	const bookmark = "https://validated-cache.test/"
	writeValidSiteFaviconCacheForTest(t, bookmark)
	source := GetSiteFaviconURL(bookmark)
	if _, _, err := ReadCachedPublicSiteFavicon(source); err != nil {
		t.Fatalf("validate cached favicon: %v", err)
	}
	release := occupySiteIconDecodeSlots(t)
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, contentType, err := FetchPublicSiteFaviconContext(ctx, source); err != nil || contentType != "image/png" {
		t.Fatalf("validated cache should bypass busy decode slots: type=%q err=%v", contentType, err)
	}
	cancel()
	if _, _, err := FetchPublicSiteFaviconContext(ctx, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("validated cache should honor cancellation: %v", err)
	}
}
