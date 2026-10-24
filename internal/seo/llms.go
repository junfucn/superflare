package seo

import (
	"html"
	"strings"

	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/i18n"
)

// linkEntry is one Markdown link emitted into llms.txt.
type linkEntry struct {
	Label       string
	URL         string
	Description string
}

// BuildLLMsTxt renders the llms.txt document described at llmstxt.org: a short
// summary followed by curated Markdown link sections. It is intentionally small
// and static — llms.txt is a discoverability convention, not a ranking factor,
// so the file stays cheap and never leaks private entries.
func BuildLLMsTxt(options model.Application, origin string) string {
	locale := i18n.NormalizeLocale(options.Locale)
	siteName := strings.TrimSpace(options.Title)
	if siteName == "" {
		siteName = fallbackSiteName
	}

	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(sanitizeMarkdownText(siteName))
	b.WriteString("\n\n")
	if options.SEONoIndex {
		b.WriteString("> This site is marked noindex by its operator. Crawlers should not index or quote it.\n\n")
		return b.String()
	}
	b.WriteString("> ")
	b.WriteString(copyFor(locale, "seo_llms_summary"))
	b.WriteString("\n\n")
	if description := resolveDescription(options, locale, PageHome); description != "" {
		b.WriteString(sanitizeMarkdownText(description))
		b.WriteString("\n\n")
	}

	b.WriteString("## ")
	b.WriteString(copyFor(locale, "seo_llms_section_pages"))
	b.WriteString("\n\n")
	for _, page := range publicPages(locale) {
		head := Build(Options{Kind: page.kind, Options: options, Path: page.path})
		writeLink(&b, linkEntry{Label: page.name, URL: joinPath(origin, page.path), Description: head.Description})
	}
	b.WriteString("\n")

	b.WriteString("## Optional\n\n")
	writeLink(&b, linkEntry{Label: "sitemap.xml", URL: joinPath(origin, sitemapXMLPath)})
	b.WriteString("\n")
	return b.String()
}

type llmsPage struct {
	path string
	kind PageKind
	name string
}

func publicPages(locale string) []llmsPage {
	pages := []llmsPage{{path: "/", kind: PageHome, name: translationOr(locale, "page_home", "Home")}}
	if !VisibilityIsPrivate() {
		pages = append(pages,
			llmsPage{path: "/applications", kind: PageApplications, name: translationOr(locale, "page_apps", "Applications")},
			llmsPage{path: "/bookmarks", kind: PageBookmarks, name: translationOr(locale, "page_bookmarks", "Bookmarks")},
		)
	}
	return append(pages, llmsPage{path: "/help", kind: PageHelp, name: translationOr(locale, "page_help", "Help")})
}

func writeLink(b *strings.Builder, entry linkEntry) {
	label := strings.TrimSpace(entry.Label)
	if label == "" || entry.URL == "" {
		return
	}
	b.WriteString("- [")
	b.WriteString(sanitizeMarkdownText(label))
	b.WriteString("](")
	b.WriteString(entry.URL)
	b.WriteString(")")
	if description := strings.TrimSpace(entry.Description); description != "" {
		b.WriteString(": ")
		b.WriteString(sanitizeMarkdownText(description))
	}
	b.WriteString("\n")
}

// sanitizeMarkdownText keeps operator-provided text from breaking out of the
// Markdown structure or injecting HTML.
func sanitizeMarkdownText(raw string) string {
	raw = html.EscapeString(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "\r", " ")
	raw = strings.ReplaceAll(raw, "\n", " ")
	return strings.Join(strings.Fields(raw), " ")
}

func translationOr(locale, key, fallback string) string {
	if value := translation(locale, key); value != "" {
		return value
	}
	return fallback
}
