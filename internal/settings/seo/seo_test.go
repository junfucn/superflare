package seo

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	settingsroot "github.com/junfuchang/superflare/internal/settings"
	"github.com/labstack/echo/v5"
)

type seoTestRenderer struct{}

func (seoTestRenderer) Render(c *echo.Context, w io.Writer, name string, data any) error {
	_, err := io.WriteString(w, `<div class="status-panel">ok</div>`)
	return err
}

func writeSEOConfigFile(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(content), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
}

func TestPageSEOReturnsTemplateOptions(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeSEOConfigFile(t, dir, "Title: SuperFlare\nLocale: zh\nTheme: blackboard\nSiteURL: https://example.com\nSEODescription: Example description\nSEOKeywords: nav, home\nSEOImage: /assets/cover.png\nSEOAuthor: Alice\nSEONoIndex: true\n")

	req := httptest.NewRequest(http.MethodGet, "/settings/seo", nil)
	rec := httptest.NewRecorder()
	e := echo.New()
	e.Renderer = seoCaptureRenderer{t: t}
	c := e.NewContext(req, rec)

	if err := pageSEO(c); err != nil {
		t.Fatalf("pageSEO: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPageSEOReturnsStyledErrorWhenConfigBroken(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeSEOConfigFile(t, dir, "Title: [broken")

	req := httptest.NewRequest(http.MethodGet, "/settings/seo", nil)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)

	if err := pageSEO(c); err != nil {
		t.Fatalf("pageSEO: %v", err)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "status-panel") {
		t.Fatalf("expected styled status page, got %s", rec.Body.String())
	}
}

func TestUpdateSEOOptionsPersistsValues(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeSEOConfigFile(t, dir, "Title: SuperFlare\nLocale: zh\nTheme: blackboard\n")

	form := url.Values{}
	form.Set("site-url", "  https://example.com/  ")
	form.Set("seo-description", "  Example description  ")
	form.Set("seo-keywords", "  nav, home  ")
	form.Set("seo-image", " /assets/cover.png ")
	form.Set("seo-author", "  Alice  ")
	form.Set("seo-noindex", "1")
	req := httptest.NewRequest(http.MethodPost, "/settings/seo", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e := echo.New()
	e.Renderer = seoTestRenderer{}
	c := e.NewContext(req, rec)

	if err := updateSEOOptions(c); err != nil {
		t.Fatalf("updateSEOOptions: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	options, err := data.GetAllSettingsOptions()
	if err != nil {
		t.Fatalf("GetAllSettingsOptions: %v", err)
	}
	if options.SiteURL != "https://example.com" {
		t.Fatalf("expected trimmed SiteURL %q, got %q", "https://example.com", options.SiteURL)
	}
	if options.SEODescription != "Example description" {
		t.Fatalf("expected trimmed SEODescription %q, got %q", "Example description", options.SEODescription)
	}
	if options.SEOKeywords != "nav, home" {
		t.Fatalf("expected trimmed SEOKeywords %q, got %q", "nav, home", options.SEOKeywords)
	}
	if options.SEOImage != "/assets/cover.png" {
		t.Fatalf("expected trimmed SEOImage %q, got %q", "/assets/cover.png", options.SEOImage)
	}
	if options.SEOAuthor != "Alice" {
		t.Fatalf("expected trimmed SEOAuthor %q, got %q", "Alice", options.SEOAuthor)
	}
	if !options.SEONoIndex {
		t.Fatal("expected SEONoIndex=true to be persisted")
	}
}

func TestUpdateSEOOptionsReturnsStyledBadRequestWhenSiteURLInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeSEOConfigFile(t, dir, "Title: SuperFlare\nLocale: zh\nTheme: blackboard\n")

	form := url.Values{}
	form.Set("site-url", "example.com")
	req := httptest.NewRequest(http.MethodPost, "/settings/seo", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)

	if err := updateSEOOptions(c); err != nil {
		t.Fatalf("updateSEOOptions: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "status-panel") {
		t.Fatalf("expected styled status page, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "site url must start with http:// or https://") {
		t.Fatalf("expected site url validation error, got %s", rec.Body.String())
	}
}

func TestUpdateSEOOptionsReturnsStyledBadRequestWhenSEOImageInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeSEOConfigFile(t, dir, "Title: SuperFlare\nLocale: zh\nTheme: blackboard\n")

	form := url.Values{}
	form.Set("seo-image", "cover.png")
	req := httptest.NewRequest(http.MethodPost, "/settings/seo", strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)

	if err := updateSEOOptions(c); err != nil {
		t.Fatalf("updateSEOOptions: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid seo image value") {
		t.Fatalf("expected seo image validation error, got %s", rec.Body.String())
	}
}

func TestUpdateSEOOptionsReturnsStyledBadRequestWhenFormDataMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/settings/seo", strings.NewReader("{"))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e := echo.New()
	c := e.NewContext(req, rec)

	if err := updateSEOOptions(c); err != nil {
		t.Fatalf("updateSEOOptions: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing form data") {
		t.Fatalf("expected missing form data detail, got %s", rec.Body.String())
	}
}

func TestPageSEOKeepsStoredRuntimeDebugModeAfterAppFlagsChange(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeSEOConfigFile(t, dir, "Title: SuperFlare\nLocale: zh\nTheme: blackboard\n")

	origFlags := define.AppFlags
	defer func() {
		define.AppFlags = origFlags
		settingsroot.SetRuntimeFlags(origFlags)
	}()

	define.AppFlags = model.Flags{DebugMode: true}
	settingsroot.SetRuntimeFlags(define.AppFlags)
	define.AppFlags = model.Flags{DebugMode: false}

	req := httptest.NewRequest(http.MethodGet, "/settings/seo", nil)
	rec := httptest.NewRecorder()
	e := echo.New()
	e.Renderer = seoDebugRenderer{t: t}
	c := e.NewContext(req, rec)

	if err := pageSEO(c); err != nil {
		t.Fatalf("pageSEO: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

type seoCaptureRenderer struct {
	t *testing.T
}

func (r seoCaptureRenderer) Render(c *echo.Context, w io.Writer, name string, data any) error {
	r.t.Helper()
	m, ok := data.(map[string]any)
	if !ok {
		r.t.Fatalf("unexpected renderer data type %T", data)
	}
	if name != "settings.html" {
		r.t.Fatalf("expected settings.html, got %q", name)
	}
	if got, _ := m["PageName"].(string); got != "SEO" {
		r.t.Fatalf("PageName = %#v, want %q", m["PageName"], "SEO")
	}
	wantStrings := map[string]string{
		"OptionSiteURL":        "https://example.com",
		"OptionSEODescription": "Example description",
		"OptionSEOKeywords":    "nav, home",
		"OptionSEOImage":       "/assets/cover.png",
		"OptionSEOAuthor":      "Alice",
	}
	for key, want := range wantStrings {
		if got, _ := m[key].(string); got != want {
			r.t.Fatalf("%s = %#v, want %q", key, m[key], want)
		}
	}
	if got, _ := m["OptionSEONoIndex"].(bool); !got {
		r.t.Fatalf("OptionSEONoIndex = %#v, want true", m["OptionSEONoIndex"])
	}
	return nil
}

type seoDebugRenderer struct {
	t *testing.T
}

func (r seoDebugRenderer) Render(c *echo.Context, w io.Writer, name string, data any) error {
	r.t.Helper()
	m, ok := data.(map[string]any)
	if !ok {
		r.t.Fatalf("unexpected renderer data type %T", data)
	}
	if got, _ := m["DebugMode"].(bool); !got {
		r.t.Fatalf("expected DebugMode=true, got %#v", m["DebugMode"])
	}
	return nil
}
