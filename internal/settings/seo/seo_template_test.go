package seo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/internal/resources/templates"
	"github.com/labstack/echo/v5"
)

// End-to-end sanity check: render settings.html through the real
// embedded template set to prove pageSEO's map satisfies settings-seo.html.
func TestPageSEORendersSettingsTemplate(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nSiteURL: https://example.com\nSEODescription: Example description\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}

	e := echo.New()
	if err := templates.RegisterRouting(e); err != nil {
		t.Fatalf("templates.RegisterRouting: %v", err)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/settings/seo", nil), rec)
	if err := pageSEO(c); err != nil {
		t.Fatalf("pageSEO: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="site-url"`,
		`name="seo-description"`,
		`name="seo-keywords"`,
		`name="seo-image"`,
		`name="seo-author"`,
		`name="seo-noindex"`,
		"https://example.com",
		"Example description",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected body to contain %q", want)
		}
	}
	if strings.Contains(body, "<no value>") {
		trimmed := body
		if len(trimmed) > 4000 {
			trimmed = trimmed[:4000]
		}
		t.Fatalf("body contains <no value>, missing template data: %s", trimmed)
	}
}
