package seo

import (
	"strings"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
)

// BuildSitemapXML renders a standards-compliant sitemap containing the public
// navigation pages. changefreq/priority are deliberately omitted: Google ignores
// both, and lastmod would only be truthful if it tracked real content edits.
// Protected pages are excluded in PRIVATE visibility mode, where crawlers would
// only ever see a login redirect.
func BuildSitemapXML(options model.Application, origin string) (string, error) {
	if options.SEONoIndex || origin == "" {
		return emptySitemap(), nil
	}

	paths := []string{define.RegularPages.Home.Path}
	if !VisibilityIsPrivate() {
		paths = append(paths, define.RegularPages.Applications.Path, define.RegularPages.Bookmarks.Path)
	}
	paths = append(paths, define.RegularPages.Help.Path)

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString("\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	b.WriteString("\n")
	for _, path := range paths {
		b.WriteString("  <url><loc>")
		b.WriteString(xmlEscape(joinPath(origin, path)))
		b.WriteString("</loc></url>\n")
	}
	b.WriteString("</urlset>\n")
	return b.String(), nil
}

func emptySitemap() string {
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n" +
		"</urlset>\n"
}

var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

// xmlEscape escapes the five XML entities. Sitemap content is limited to URLs,
// so this is sufficient.
func xmlEscape(raw string) string {
	return xmlEscaper.Replace(raw)
}
