package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// homePageBenchConfig is a settings fixture that exercises the expensive parts of a
// home page render: the bookmark icon fallback chain (IconMode FILLING with no
// configured icons) and both bookmark and app sections enabled.
const homePageBenchConfig = `Title: SuperFlare
SiteIconMode: mdi
OpenAppNewTab: true
OpenBookmarkNewTab: true
ShowTitle: false
ShowSearchComponent: true
SearchMode: bookmarks
SearchEngine: bing
SearchEngineOpenMode: new-tab
ShowDateTime: true
ShowApps: true
ShowFavorites: true
ShowBookmarks: true
Theme: onedark
ThemeBase: onedark
EnableEncryptedLink: false
IconMode: FILLING
Locale: zh
LoginUser: admin
LoginPass: admin
BackgroundImageMode: url
BackgroundOpacity: 100
GlassEffect: none
GlassIntensity: 30
HomeMaxColumns: 5
HomeMaxWidth: 1600
`

// benchmarkBookmarkFixture builds a bookmark file at the scale of a large
// personal dashboard: 12 categories and 59 links, none of which configure an
// explicit icon, so every single one runs the site favicon fallback path.
func benchmarkBookmarkFixture() string {
	var b strings.Builder
	b.WriteString("categories:\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "- id: \"%d\"\n  title: \"Category %d\"\n", i, i)
	}
	b.WriteString("links:\n")
	for i := 0; i < 59; i++ {
		fmt.Fprintf(&b, `- name: "Bookmark %d"
  link: "https://site-%d.example.com/path"
  category: "%d"
`, i, i, i%12+1)
	}
	return b.String()
}

const benchmarkAppsFixture = `links:
- name: App 1
  link: https://app-1.example.com
- name: App 2
  link: https://app-2.example.com
- name: App 3
  link: https://app-3.example.com
- name: App 4
  link: https://app-4.example.com
- name: App 5
  link: https://app-5.example.com
- name: App 6
  link: https://app-6.example.com
`

// setupHomePageBenchWorkingDir writes self-contained data fixtures into a temp
// working directory so the benchmark does not depend on a local deployment's
// (gitignored) configuration files.
func setupHomePageBenchWorkingDir(b *testing.B) {
	b.Helper()
	origWd, err := os.Getwd()
	require.NoError(b, err)
	tmpDir := b.TempDir()
	require.NoError(b, os.Chdir(tmpDir))
	b.Cleanup(func() {
		_ = os.Chdir(origWd)
	})

	origEnv := os.Getenv("FLARE_BASELINE")
	require.NoError(b, os.Setenv("FLARE_BASELINE", "1"))
	b.Cleanup(func() {
		if origEnv == "" {
			_ = os.Unsetenv("FLARE_BASELINE")
		} else {
			_ = os.Setenv("FLARE_BASELINE", origEnv)
		}
	})

	fixtures := map[string]string{
		"config.yml":    homePageBenchConfig,
		"apps.yml":      benchmarkAppsFixture,
		"bookmarks.yml": benchmarkBookmarkFixture(),
		"ports.yaml":    "ports: []\n",
	}
	for name, content := range fixtures {
		require.NoError(b, os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0o644))
	}
}

func newHomePageBenchRouter(b *testing.B) http.Handler {
	b.Helper()
	flags := newTestFlags(true, "DEFAULT", false)
	handler, err := NewRouter(&flags)
	require.NoError(b, err)
	return handler
}

// BenchmarkHomePageRender measures a full GET / round trip through the real
// router and middleware stack. It guards the hot bookmark rendering path:
// embedded asset versioning, favicon resolution for 59 icon-less bookmarks and
// template rendering.
func BenchmarkHomePageRender(b *testing.B) {
	setupHomePageBenchWorkingDir(b)
	handler := newHomePageBenchRouter(b)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "identity")

	// Warm up template parsing and config caches.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(b, http.StatusOK, rec.Code)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
		_, _ = io.Copy(io.Discard, rec.Body)
	}
}
