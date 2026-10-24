package seo

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/i18n"
)

// PageKind identifies which logical page a head block is being built for. It
// selects the default description/keywords and drives structured-data shape.
type PageKind string

const (
	PageHome         PageKind = "home"
	PageApplications PageKind = "apps"
	PageBookmarks    PageKind = "bookmarks"
	PageHelp         PageKind = "help"
)

// Head is the fully resolved set of SEO tags for one rendered page.
type Head struct {
	Title       string
	Description string
	Keywords    string
	Author      string
	Canonical   string
	Image       string
	ImageAlt    string
	Robots      string
	Type        string
	Locale      string
	JSONLD      template.JS
}

// Options carries the inputs required to build a Head. All fields are optional;
// zero values fall back to sensible defaults.
type Options struct {
	// Kind selects the default description and structured data.
	Kind PageKind
	// Options is the loaded application configuration.
	Options model.Application
	// Request is the incoming HTTP request (used to infer the origin).
	Request *http.Request
	// Path is the canonical path of the page, e.g. "/applications".
	Path string
	// Name is the visible page name, e.g. "Applications"; empty for home.
	Name string
	// NoIndex forces a robots noindex header for this page.
	NoIndex bool
	// Items is the curated link set rendered on the page. It feeds the
	// ItemList structured data that tells assistants what the page contains.
	Items []Link
}

// Build resolves the head block for a page.
func Build(input Options) Head {
	options := input.Options
	locale := i18n.NormalizeLocale(options.Locale)
	origin := ResolveOrigin(options, input.Request)
	path := input.Path
	if path == "" {
		path = pagesHomePath
	}

	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = fallbackSiteName
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		title = title + separator + name
	}

	head := Head{
		Title:       title,
		Description: resolveDescription(options, locale, input.Kind),
		Keywords:    resolveKeywords(options, locale),
		Author:      strings.TrimSpace(options.SEOAuthor),
		Canonical:   joinPath(origin, path),
		Image:       resolveImage(options, origin),
		Robots:      resolveRobots(options, input.NoIndex),
		Type:        resolveType(input.Kind),
		Locale:      openGraphLocale(locale),
	}
	if head.Image != "" {
		head.ImageAlt = title
	}
	head.JSONLD = buildJSONLD(input, head, origin, locale)
	return head
}

func resolveDescription(options model.Application, locale string, kind PageKind) string {
	if custom := strings.TrimSpace(options.SEODescription); custom != "" {
		return custom
	}
	key := "seo_desc_home"
	switch kind {
	case PageApplications:
		key = "seo_desc_apps"
	case PageBookmarks:
		key = "seo_desc_bookmarks"
	case PageHelp:
		key = "seo_desc_help"
	}
	return copyFor(locale, key)
}

func resolveKeywords(options model.Application, locale string) string {
	if custom := strings.TrimSpace(options.SEOKeywords); custom != "" {
		return custom
	}
	return copyFor(locale, "seo_keywords_default")
}

func resolveImage(options model.Application, origin string) string {
	image := strings.TrimSpace(options.SEOImage)
	if image == "" {
		return ""
	}
	return AbsoluteURL(origin, image)
}

func resolveRobots(options model.Application, noIndex bool) string {
	// PRIVATE visibility hides /applications and /bookmarks behind auth, so those
	// pages must not be indexed even when the operator left SEONoIndex off.
	if options.SEONoIndex || noIndex || VisibilityIsPrivate() {
		return robotsNoIndex
	}
	return robotsIndex
}

// resolveType keeps every page as og:type "website": the navigation pages are
// not articles, and "profile" would misrepresent a service index.
func resolveType(kind PageKind) string {
	return "website"
}

func openGraphLocale(locale string) string {
	if i18n.NormalizeLocale(locale) == "en" {
		return "en_US"
	}
	return "zh_CN"
}
