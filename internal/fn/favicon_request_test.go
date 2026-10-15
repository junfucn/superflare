package fn

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSiteFaviconRequestsUseBrowserCompatibleUserAgent(t *testing.T) {
	withSiteIconTempWorkingDir(t)
	icon := encodeTestICODIB()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := r.UserAgent()
		if !strings.HasPrefix(agent, "Mozilla/5.0 ") || !strings.Contains(agent, "AppleWebKit/") || strings.Contains(strings.ToLower(agent), "fetcher") {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/static/site.ico"></head></html>`))
		case "/static/site.ico":
			w.Header().Set("Content-Type", "image/x-icon")
			_, _ = w.Write(icon)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	data, contentType, err := FetchPublicSiteFavicon(upstream.URL + "/favicon.ico")
	if err != nil || contentType != "image/x-icon" || !bytes.Equal(data, icon) {
		t.Fatalf("browser-compatible favicon discovery: type=%q err=%v", contentType, err)
	}
}
