package redir

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/labstack/echo/v5"
)

func TestSavedRedirectsWithBase64SymbolsAndDynamicURLs(t *testing.T) {
	t.Chdir(t.TempDir())
	const sourceURL = "https://example.com/~"
	const localURL = "http://192.168.1.20/~"
	if err := data.EnsureAppConfigExists(); err != nil {
		t.Fatal(err)
	}
	if err := define.UpdatePagePalettes(); err != nil {
		t.Fatal(err)
	}
	if err := data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{
		{Name: "Encoded", URL: sourceURL, LocalURL: localURL},
		{Name: "Proxy", URL: "{origin}/app"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := data.SaveNormalBookmarks(model.Bookmarks{Items: []model.Bookmark{
		{Name: "IPv6", URL: "{protocol}//{hostname}:5000/"},
	}}); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	RegisterRouting(e)
	for _, tc := range []struct {
		name, origin, path, target, local, peer, proto string
		status                                         int
	}{
		{"encoded app", "http://nav.example.test", "/redir/url", sourceURL, "", "8.8.8.8:1234", "", http.StatusFound},
		{"IPv6 bookmark", "http://[fd00::1]:3636", "/redir/url", "http://[fd00::1]:5000/", "", "[fd00::2]:1234", "", http.StatusFound},
		{"HTTPS proxy", "http://nav.example.test", "/redir/url", "https://nav.example.test/app", "", "127.0.0.1:1234", "https", http.StatusFound},
		{"local public detection", "http://nav.example.test", "/redir/local", sourceURL, localURL, "8.8.8.8:1234", "", http.StatusOK},
		{"local probe page", "http://192.168.1.10:3636", "/redir/local", sourceURL, localURL, "192.168.1.30:1234", "", http.StatusOK},
		{"unsaved target", "http://nav.example.test", "/redir/url", "https://other.example.com/~", "", "8.8.8.8:1234", "", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address := tc.origin + tc.path + "?go=" + data.Base64EncodeUrl(tc.target)
			if tc.local != "" {
				address += "&local=" + data.Base64EncodeUrl(tc.local)
			}
			req := httptest.NewRequest(http.MethodGet, address, nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-Proto", tc.proto)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == http.StatusFound && rec.Header().Get("Location") != tc.target {
				t.Fatalf("location=%q, want %q", rec.Header().Get("Location"), tc.target)
			}
			if tc.status == http.StatusOK {
				for _, target := range []string{tc.target, tc.local} {
					if !strings.Contains(rec.Body.String(), target) {
						t.Fatalf("local probe page missing %q", target)
					}
				}
			}
		})
	}
}
