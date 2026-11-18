package seo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/netutil"
)

func testOptions(locale string) model.Application {
	return model.Application{
		Title:         "SuperFlare",
		Locale:        locale,
		ShowApps:      true,
		ShowBookmarks: true,
	}
}

func TestResolveOriginPrefersConfiguredSiteURL(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://internal.localhost:8080/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Host = "internal.localhost:8080"
	request.Header.Set("X-Forwarded-Host", "different.example:9443")
	request.Header.Set("X-Forwarded-Proto", "https")

	options := testOptions("zh")
	options.SiteURL = "https://flare.example.com/"

	if got := ResolveOrigin(options, request); got != "https://flare.example.com" {
		t.Fatalf("expected configured site url to win, got %q", got)
	}
}

func TestResolveOriginHonoursForwardedHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://10.0.0.5:8080/", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Host = "10.0.0.5:8080"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-Host", "flare.example.com")

	if got := ResolveOrigin(testOptions("zh"), request); got != "https://flare.example.com" {
		t.Fatalf("expected forwarded origin, got %q", got)
	}
}

func TestResolveOriginOnlyAcceptsForwardedHostFromTrustedProxy(t *testing.T) {
	for _, tc := range []struct {
		name, trusted, peer, host, want string
	}{
		{"untrusted-public", "", "93.184.216.34:1234", "forwarded.example:9443", "http://nav.example.test"},
		{"untrusted-private", "", "192.168.1.2:1234", "forwarded.example:9443", "http://nav.example.test"},
		{"disabled-loopback", "none", "127.0.0.1:1234", "forwarded.example:9443", "http://nav.example.test"},
		{"trusted-loopback", "", "127.0.0.1:1234", " Forwarded.Example:9443 , other.example", "https://forwarded.example:9443"},
		{"configured-cidr", "10.1.0.0/16", "10.1.2.3:1234", "forwarded.example:9443", "https://forwarded.example:9443"},
		{"outside-cidr", "10.1.0.0/16", "10.2.2.3:1234", "forwarded.example:9443", "http://nav.example.test"},
		{"unsafe-trusted-host", "", "127.0.0.1:1234", "evil.example/path", "https://nav.example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxies, err := netutil.ParseTrustedProxies(tc.trusted)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, "http://nav.example.test/", nil)
			request.RemoteAddr = tc.peer
			request.Header.Set("X-Forwarded-Host", tc.host)
			request.Header.Set("X-Forwarded-Proto", "https")
			if got := ResolveOrigin(testOptions("zh"), netutil.WithTrustedProxies(request, proxies)); got != tc.want {
				t.Fatalf("origin=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveOriginRejectsUnsafeForwardedHost(t *testing.T) {
	for _, host := range []string{
		`evil.example.com"><script>`,
		"evil.example/path",
		"evil.example:",
		"evil.example:bad",
		"evil.example:65536",
		"[fe80::1%eth0]:8080",
	} {
		t.Run(host, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://10.0.0.5:8080/", nil)
			request.Host = "10.0.0.5:8080"
			request.RemoteAddr = "127.0.0.1:1234"
			request.Header.Set("X-Forwarded-Host", host)

			if got := ResolveOrigin(testOptions("zh"), request); got != "http://10.0.0.5:8080" {
				t.Fatalf("expected fallback host, got %q", got)
			}
		})
	}
}

func TestNormalizeSiteURL(t *testing.T) {
	cases := map[string]string{
		"https://flare.example.com/":        "https://flare.example.com",
		" https://flare.example.com/base/":  "https://flare.example.com/base",
		"ftp://flare.example.com":           "",
		"flare.example.com":                 "",
		"":                                  "",
		"javascript:alert(1)":               "",
		"https://user:password@example.com": "",
		"https://example.com:65536":         "",
		"https://example.com:":              "",
		"HTTPS://EXAMPLE.COM:443/base/":     "https://EXAMPLE.COM:443/base",
		"https://example.com/a%3Fb/%2F/":    "https://example.com/a%3Fb/%2F",
	}
	for input, want := range cases {
		if got := NormalizeSiteURL(input); got != want {
			t.Errorf("NormalizeSiteURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildHeadHomeDefaults(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://flare.example.com/", nil)
	head := Build(Options{
		Kind:    PageHome,
		Options: testOptions("zh"),
		Request: request,
		Path:    "/",
	})

	if head.Canonical != "https://flare.example.com/" {
		t.Fatalf("unexpected canonical: %q", head.Canonical)
	}
	if !strings.HasPrefix(head.Robots, "index") {
		t.Fatalf("expected indexable robots, got %q", head.Robots)
	}
	if head.Description == "" {
		t.Fatal("expected a default description")
	}
	if head.Locale != "zh_CN" {
		t.Fatalf("unexpected og locale: %q", head.Locale)
	}
	if head.JSONLD == "" {
		t.Fatal("expected json-ld payload")
	}
}

func TestBuildHeadSubpageTitleAndNoIndex(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://flare.example.com/bookmarks", nil)
	head := Build(Options{
		Kind:    PageBookmarks,
		Options: testOptions("en"),
		Request: request,
		Path:    "/bookmarks",
		Name:    "Bookmarks",
		NoIndex: true,
	})

	if head.Title != "SuperFlare · Bookmarks" {
		t.Fatalf("unexpected title: %q", head.Title)
	}
	if head.Robots != robotsNoIndex {
		t.Fatalf("expected noindex robots, got %q", head.Robots)
	}
	if head.Locale != "en_US" {
		t.Fatalf("unexpected og locale: %q", head.Locale)
	}
}

func TestBuildJSONLDIsValidJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://flare.example.com/", nil)
	head := Build(Options{
		Kind:    PageHome,
		Options: testOptions("zh"),
		Request: request,
		Path:    "/",
		Items: []Link{
			{Name: "Navidrome", URL: "https://music.example.com", Description: "Music"},
			{Name: "", URL: "https://ignored.example.com"},
		},
	})

	var payload map[string]any
	if err := json.Unmarshal([]byte(head.JSONLD), &payload); err != nil {
		t.Fatalf("json-ld is not valid json: %v", err)
	}
	graph, ok := payload["@graph"].([]any)
	if !ok || len(graph) == 0 {
		t.Fatalf("expected a non-empty @graph, got %#v", payload["@graph"])
	}
	if strings.Contains(string(head.JSONLD), "<") {
		t.Fatal("json-ld must not contain a raw '<'")
	}
}

func TestBuildSitemapXML(t *testing.T) {
	SetRuntimeFlags(model.Flags{Visibility: "DEFAULT"})
	body, err := BuildSitemapXML(testOptions("zh"), "https://flare.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"<loc>https://flare.example.com/</loc>",
		"<loc>https://flare.example.com/applications</loc>",
		"<loc>https://flare.example.com/bookmarks</loc>",
		"<loc>https://flare.example.com/help</loc>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sitemap missing %q", want)
		}
	}
	if strings.Contains(body, "changefreq") || strings.Contains(body, "priority") {
		t.Error("sitemap should not emit ignored changefreq/priority hints")
	}
}

func TestBuildSitemapXMLPrivateInstanceHidesProtectedPages(t *testing.T) {
	SetRuntimeFlags(model.Flags{Visibility: "PRIVATE"})
	t.Cleanup(func() { SetRuntimeFlags(model.Flags{Visibility: "DEFAULT"}) })

	body, err := BuildSitemapXML(testOptions("zh"), "https://flare.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, unwanted := range []string{"/applications", "/bookmarks"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("private sitemap must not advertise %q", unwanted)
		}
	}
	if !strings.Contains(body, "<loc>https://flare.example.com/</loc>") {
		t.Error("private sitemap should still list the home page")
	}
}

func TestBuildRobotsTxtPublicInstance(t *testing.T) {
	SetRuntimeFlags(model.Flags{Visibility: "DEFAULT"})
	body := BuildRobotsTxt(testOptions("zh"), "https://flare.example.com")

	for _, want := range []string{
		"User-agent: *",
		"Disallow: /settings",
		"Disallow: /editor",
		"User-agent: GPTBot",
		"User-agent: ClaudeBot",
		"Sitemap: https://flare.example.com/sitemap.xml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("robots.txt missing %q\n%s", want, body)
		}
	}
}

func TestBuildRobotsTxtPrivateInstanceBlocksEverything(t *testing.T) {
	SetRuntimeFlags(model.Flags{Visibility: "PRIVATE"})
	t.Cleanup(func() { SetRuntimeFlags(model.Flags{Visibility: "DEFAULT"}) })

	body := BuildRobotsTxt(testOptions("zh"), "https://flare.example.com")
	if !strings.Contains(body, "Disallow: /") {
		t.Fatalf("expected a blanket disallow, got:\n%s", body)
	}
	if strings.Contains(body, "/settings") {
		t.Fatal("private robots.txt must not enumerate internal paths")
	}
}

func TestBuildRobotsTxtNoIndexOption(t *testing.T) {
	SetRuntimeFlags(model.Flags{Visibility: "DEFAULT"})
	options := testOptions("zh")
	options.SEONoIndex = true

	body := BuildRobotsTxt(options, "https://flare.example.com")
	if strings.Contains(body, "Sitemap:") {
		t.Fatal("noindex instance must not advertise a sitemap")
	}
}

func TestBuildLLMsTxtListsPublicPages(t *testing.T) {
	SetRuntimeFlags(model.Flags{Visibility: "DEFAULT"})
	body := BuildLLMsTxt(testOptions("zh"), "https://flare.example.com")

	for _, want := range []string{
		"# SuperFlare",
		"## 页面",
		"(https://flare.example.com/)",
		"(https://flare.example.com/applications)",
		"(https://flare.example.com/bookmarks)",
		"(https://flare.example.com/help)",
		"sitemap.xml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("llms.txt missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "llms-full.txt") {
		t.Error("llms-full.txt is not part of the v2 spec and must not be advertised")
	}
}

func TestBuildLLMsTxtPrivateInstanceHidesProtectedPages(t *testing.T) {
	SetRuntimeFlags(model.Flags{Visibility: "PRIVATE"})
	t.Cleanup(func() { SetRuntimeFlags(model.Flags{Visibility: "DEFAULT"}) })

	body := BuildLLMsTxt(testOptions("zh"), "https://flare.example.com")
	for _, unwanted := range []string{"/applications", "/bookmarks"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("private llms.txt must not advertise %q", unwanted)
		}
	}
}

func TestApplyTemplateDataAlwaysSetsKeys(t *testing.T) {
	m := map[string]any{}
	ApplyTemplateData(m, Head{Title: "T", Canonical: "https://example.com/"})

	for _, key := range []string{
		"SEOTitle", "SEODescription", "SEOKeywords", "SEOAuthor",
		"SEOCanonical", "SEOImage", "SEOImageAlt", "SEORobots",
		"SEOType", "SEOLocale", "SEOJSONLD",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("template key %q was not set", key)
		}
	}
}

func TestPageOptionsUsesConfiguredRoutePaths(t *testing.T) {
	options := testOptions("en")

	apps := PageOptions(options, nil, PageApplications)
	if apps.Path != "/applications" || apps.Name != "Applications" {
		t.Fatalf("unexpected apps options: %#v", apps)
	}
	bookmarks := PageOptions(options, nil, PageBookmarks)
	if bookmarks.Path != "/bookmarks" || bookmarks.Name != "Bookmarks" {
		t.Fatalf("unexpected bookmarks options: %#v", bookmarks)
	}
	home := PageOptions(options, nil, PageHome)
	if home.Path != "/" || home.Name != "" {
		t.Fatalf("unexpected home options: %#v", home)
	}
}
