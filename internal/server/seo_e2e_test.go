package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seoGet(t *testing.T, h http.Handler, path string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "flare.example.com"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(body), rec.Result().Header
}

func TestNewRouter_SEODiscoveryEndpointsAndHeadTags(t *testing.T) {
	withRouterTestWorkingDir(t)

	flags := newTestFlags(true, "DEFAULT", false)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)

	pages := []struct {
		path     string
		contains []string
	}{
		{
			path: "/",
			contains: []string{
				`<link rel="canonical" href="http://flare.example.com/">`,
				`<meta property="og:url" content="http://flare.example.com/">`,
				`<meta name="twitter:card"`,
				`<script type="application/ld+json">`,
				`"@type":"WebSite"`,
				`"@type":"ItemList"`,
				`<a class="skip-link" href="#main-content">`,
				`<main class="container no-select" id="main-content">`,
				`role="search"`,
			},
		},
		{
			path: "/applications",
			contains: []string{
				`<link rel="canonical" href="http://flare.example.com/applications">`,
				`"@type":"BreadcrumbList"`,
				`"@type":"WebPage"`,
			},
		},
		{
			path:     "/bookmarks",
			contains: []string{`<link rel="canonical" href="http://flare.example.com/bookmarks">`},
		},
		{
			path:     "/help",
			contains: []string{`<link rel="canonical" href="http://flare.example.com/help">`},
		},
	}
	for _, tc := range pages {
		code, body, _ := seoGet(t, handler, tc.path)
		require.Equal(t, http.StatusOK, code, "%s should render", tc.path)
		for _, want := range tc.contains {
			assert.Contains(t, body, want, "%s should include SEO markup", tc.path)
		}
	}

	code, robots, header := seoGet(t, handler, "/robots.txt")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, header.Get("Content-Type"), "text/plain")
	for _, want := range []string{
		"User-agent: *",
		"Disallow: /settings",
		"Disallow: /editor",
		"User-agent: GPTBot",
		"User-agent: ClaudeBot",
		"Sitemap: http://flare.example.com/sitemap.xml",
	} {
		assert.Contains(t, robots, want, "robots.txt should include %q", want)
	}

	code, sitemap, header := seoGet(t, handler, "/sitemap.xml")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, header.Get("Content-Type"), "application/xml")
	assert.Contains(t, sitemap, `<loc>http://flare.example.com/</loc>`)
	assert.Contains(t, sitemap, `<loc>http://flare.example.com/applications</loc>`)
	assert.Contains(t, sitemap, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)

	code, llms, header := seoGet(t, handler, "/llms.txt")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, header.Get("Content-Type"), "text/plain")
	assert.Contains(t, llms, "# SuperFlare")
	assert.Contains(t, llms, "sitemap.xml")
	assert.NotContains(t, llms, "llms-full.txt")
}

func TestNewRouter_SEOSettingsPageIsNoIndex(t *testing.T) {
	withRouterTestWorkingDir(t)

	flags := newTestFlags(true, "DEFAULT", false)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)

	code, body, _ := seoGet(t, handler, "/settings/seo")
	require.Equal(t, http.StatusOK, code)
	for _, want := range []string{
		`<meta name="robots" content="noindex, nofollow">`,
		`name="site-url"`,
		`name="seo-description"`,
		`name="seo-keywords"`,
		`name="seo-image"`,
		`name="seo-author"`,
		`name="seo-noindex"`,
	} {
		assert.Contains(t, body, want, "SEO settings page should include %q", want)
	}

	code, notFound, _ := seoGet(t, handler, "/definitely-not-a-page")
	require.Equal(t, http.StatusNotFound, code)
	assert.Contains(t, notFound, `<meta name="robots" content="noindex">`)
}

func TestNewRouter_SEOPrivateInstanceHidesProtectedPages(t *testing.T) {
	withRouterTestWorkingDir(t)

	flags := newTestFlags(false, "PRIVATE", false)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)

	_, sitemap, _ := seoGet(t, handler, "/sitemap.xml")
	assert.Contains(t, sitemap, `<loc>http://flare.example.com/</loc>`)
	assert.NotContains(t, sitemap, "/applications")
	assert.NotContains(t, sitemap, "/bookmarks")

	_, robots, _ := seoGet(t, handler, "/robots.txt")
	assert.Contains(t, robots, "Disallow: /")
	assert.NotContains(t, robots, "/settings")

	_, home, _ := seoGet(t, handler, "/")
	assert.Contains(t, home, `<meta name="robots" content="noindex, nofollow">`)
}

func TestNewRouter_SEOHeadRespectsConfiguredSiteURL(t *testing.T) {
	withRouterTestWorkingDir(t)

	// The configured site URL must win over the request host, which is the only
	// way to emit stable canonical links behind a reverse proxy.
	require.NoError(t, writeTestAppConfig(seoTestAppConfig))

	flags := newTestFlags(true, "DEFAULT", false)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)

	_, body, _ := seoGet(t, handler, "/")
	assert.Contains(t, body, `<link rel="canonical" href="https://flare.example.com/">`)
	assert.Contains(t, body, `<meta property="og:url" content="https://flare.example.com/">`)
}

func writeTestAppConfig(raw string) error {
	return os.WriteFile("config.yml", []byte(raw), 0o644)
}

const seoTestAppConfig = `Title: SuperFlare
SiteURL: https://flare.example.com
SEODescription: Custom description for testing
SEOKeywords: alpha,beta
SEOAuthor: Tester
ShowTitle: true
ShowSearchComponent: true
ShowDateTime: true
ShowApps: true
ShowFavorites: true
ShowBookmarks: true
Theme: onedark
Locale: zh
`
