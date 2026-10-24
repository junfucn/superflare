package fn

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFetchPublicSiteFaviconServesIconWhenCacheWriteFails(t *testing.T) {
	withSiteIconTempWorkingDir(t)

	iconURL := "https://unwritable-cache.example.com/favicon.ico"
	siteIconFailures.Delete(siteFaviconCacheKey(iconURL))
	siteIconMemory.Delete(iconURL)
	defer siteIconMemory.Delete(iconURL)

	oldClient := siteIconHTTPClient
	defer func() { siteIconHTTPClient = oldClient }()

	requestCount := 0
	siteIconHTTPClient = &http.Client{
		Timeout: 2 * time.Second,
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader(string(encodeTestFavicon(t, "png")))),
				Request:    req,
			}, nil
		}),
	}

	oldWrite := writeSiteFaviconCache
	writeSiteFaviconCache = func(string, []byte) error {
		return errors.New("access is denied")
	}
	defer func() { writeSiteFaviconCache = oldWrite }()

	data, contentType, err := FetchPublicSiteFavicon(iconURL)
	if err != nil {
		t.Fatalf("cache write failure must not fail the fetch: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("content type = %q", contentType)
	}
	if len(data) == 0 {
		t.Fatal("fetched data is empty")
	}

	// The second request must be served from the in-memory fallback instead of
	// hitting the network again.
	if _, _, err := FetchPublicSiteFavicon(iconURL); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("memory cache should serve the second request, HTTP requests = %d", requestCount)
	}

	// A memory-served icon must count as validated so page renders emit the
	// direct proxy image instead of the fallback plus async marker.
	if !hasValidatedSiteFaviconFast(iconURL) {
		t.Fatal("memory cached icon should be reported as validated")
	}
}

func TestSiteIconMemoryCacheBoundsEntries(t *testing.T) {
	cache := &siteIconMemoryCache{}
	now := time.Now()
	data := encodeTestFavicon(t, "png")
	for i := 0; i < siteIconMemoryMaxEntries+16; i++ {
		key := "https://bounded-" + strings.Repeat("x", i%7) + "-" + time.Duration(i).String() + ".example.com/favicon.ico"
		cache.Store(key, data, "image/png", now)
	}
	if got := cache.Len(); got > siteIconMemoryMaxEntries {
		t.Fatalf("memory cache entries = %d, want <= %d", got, siteIconMemoryMaxEntries)
	}
}

func TestSiteIconMemoryCacheExpires(t *testing.T) {
	cache := &siteIconMemoryCache{}
	start := time.Now()
	data := encodeTestFavicon(t, "png")
	key := "https://expiring.example.com/favicon.ico"
	cache.Store(key, data, "image/png", start)
	if _, _, ok := cache.Load(key, start.Add(siteIconMemoryTTL-time.Second)); !ok {
		t.Fatal("unexpired entry should be served")
	}
	if _, _, ok := cache.Load(key, start.Add(siteIconMemoryTTL)); ok {
		t.Fatal("expired entry should not be served")
	}
}

func TestResolveSiteFaviconSourceMatchesLegacyParsing(t *testing.T) {
	cases := []struct {
		link    string
		wantURL string
	}{
		{"https://example.com/path", "https://example.com/favicon.ico"},
		{"http://example.com:8080/path", "http://example.com:8080/favicon.ico"},
		{"  https://example.com  ", "https://example.com/favicon.ico"},
		{"not-a-valid-url", ""},
		{"/relative/path", ""},
		{"://invalid", ""},
		{"", ""},
	}
	for _, tc := range cases {
		gotURL, _ := resolveSiteFaviconSource(tc.link)
		if gotURL != tc.wantURL {
			t.Fatalf("resolveSiteFaviconSource(%q) = %q, want %q", tc.link, gotURL, tc.wantURL)
		}
		if legacy := GetSiteFaviconURL(tc.link); legacy != gotURL {
			t.Fatalf("GetSiteFaviconURL(%q) = %q, want %q", tc.link, legacy, gotURL)
		}
	}
}

func TestGetSiteFaviconAssetURLFastUsesMemoryCache(t *testing.T) {
	withSiteIconTempWorkingDir(t)
	iconURL := "https://memory-fast.example.com/favicon.ico"
	siteIconMemory.Delete(iconURL)
	defer siteIconMemory.Delete(iconURL)

	if got := GetSiteFaviconAssetURLFast("https://memory-fast.example.com/path"); got != "" {
		t.Fatalf("fast lookup before download = %q, want empty", got)
	}
	siteIconMemory.Store(iconURL, encodeTestFavicon(t, "png"), "image/png", time.Now())
	want := siteIconProxyURL(iconURL)
	if got := GetSiteFaviconAssetURLFast("https://memory-fast.example.com/path"); got != want {
		t.Fatalf("fast lookup with memory cache = %q, want %q", got, want)
	}
}

func TestFetchPublicSiteFaviconMemoryFallbackHonorsCancelledContext(t *testing.T) {
	withSiteIconTempWorkingDir(t)
	iconURL := "https://memory-context.example.com/favicon.ico"
	siteIconMemory.Delete(iconURL)
	defer siteIconMemory.Delete(iconURL)
	siteIconMemory.Store(iconURL, encodeTestFavicon(t, "png"), "image/png", time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if data, _, err := FetchPublicSiteFaviconContext(ctx, iconURL); err == nil && len(data) > 0 {
		t.Fatal("memory fallback should still respect a cancelled request context")
	}
}
