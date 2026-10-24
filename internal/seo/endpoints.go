package seo

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/statuspage"
)

const (
	robotsTxtPath  = "/robots.txt"
	sitemapXMLPath = "/sitemap.xml"
	llmsTxtPath    = "/llms.txt"

	contentTypeText = "text/plain; charset=utf-8"
	contentTypeXML  = "application/xml; charset=utf-8"
	// Discovery endpoints are cheap to compute but depend on the current
	// configuration, so a short shared cache is better than regenerating on
	// every crawl with no caching at all.
	publicCacheControl = "public, max-age=3600"
)

// RegisterRouting wires the discovery endpoints crawlers fetch. They are public
// on purpose: a crawler cannot authenticate, and hiding the files just makes the
// site invisible to search and AI answer engines.
func RegisterRouting(e *echo.Echo) {
	e.GET(robotsTxtPath, renderRobotsTxt)
	e.GET(sitemapXMLPath, renderSitemapXML)
	e.GET(llmsTxtPath, renderLLMsTxt)
}

// loadOptions resolves the current configuration, rendering the shared styled
// error page when the config cannot be read.
func loadOptions(c *echo.Context) (model.Application, bool) {
	options, err := data.GetAllSettingsOptions()
	if err != nil {
		statuspage.BindOptionsLoadError(c, err)
		_ = statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
		return model.Application{}, false
	}
	statuspage.BindOptions(c, options)
	return options, true
}

func writeText(c *echo.Context, contentType string, body string, cacheControl string) error {
	c.Response().Header().Set("Cache-Control", cacheControl)
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, contentType, []byte(body))
}

func renderRobotsTxt(c *echo.Context) error {
	options, ok := loadOptions(c)
	if !ok {
		return nil
	}
	return writeText(c, contentTypeText, BuildRobotsTxt(options, ResolveOrigin(options, c.Request())), publicCacheControl)
}

func renderSitemapXML(c *echo.Context) error {
	options, ok := loadOptions(c)
	if !ok {
		return nil
	}
	body, err := BuildSitemapXML(options, ResolveOrigin(options, c.Request()))
	if err != nil {
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
	}
	return writeText(c, contentTypeXML, body, publicCacheControl)
}

func renderLLMsTxt(c *echo.Context) error {
	options, ok := loadOptions(c)
	if !ok {
		return nil
	}
	return writeText(c, contentTypeText, BuildLLMsTxt(options, ResolveOrigin(options, c.Request())), publicCacheControl)
}

// adminPaths are the operator-only surfaces that must never be crawled. They are
// only published on instances that opted into indexing: enumerating paths on a
// private instance would leak the shape of the deployment.
var adminPaths = []string{"/settings", "/settings/", "/editor", "/login", "/logout", "/ping", "/redir", "/redir/"}

// trainingCrawlers only feed model training corpora. Search and answer engines
// are left alone (they fall through to the wildcard group) because those are the
// ones that actually send visitors.
var trainingCrawlers = []string{
	"GPTBot",
	"ClaudeBot",
	"CCBot",
	"Bytespider",
	"meta-externalagent",
	"Google-Extended",
	"Applebot-Extended",
}

// BuildRobotsTxt renders robots.txt. The default posture is conservative: a
// private instance blocks everything with a bare rule, because robots.txt is
// world-readable and any path list would leak the internal service map.
func BuildRobotsTxt(options model.Application, origin string) string {
	var b strings.Builder
	if VisibilityIsPrivate() || options.SEONoIndex {
		b.WriteString("# Private instance: keep the whole host out of every index.\n")
		b.WriteString("User-agent: *\n")
		b.WriteString("Disallow: /\n")
		return b.String()
	}

	b.WriteString("# Public instance: crawlable startpage, operator surfaces excluded.\n")
	b.WriteString("User-agent: *\n")
	b.WriteString("Allow: /\n")
	for _, path := range adminPaths {
		b.WriteString("Disallow: ")
		b.WriteString(path)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString("# Training-only crawlers are opted out; search/answer crawlers stay allowed.\n")
	for _, agent := range trainingCrawlers {
		b.WriteString("User-agent: ")
		b.WriteString(agent)
		b.WriteString("\n")
		b.WriteString("Disallow: /\n")
	}
	b.WriteString("\n")
	if origin != "" {
		b.WriteString("Sitemap: ")
		b.WriteString(joinPath(origin, sitemapXMLPath))
		b.WriteString("\n")
		b.WriteString("# LLM-friendly index: ")
		b.WriteString(joinPath(origin, llmsTxtPath))
		b.WriteString("\n")
	}
	return b.String()
}
