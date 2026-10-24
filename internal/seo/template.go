package seo

import (
	"net/http"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/i18n"
)

// ApplyTemplateData writes the SEO template fields consumed by
// embed/templates/home.html onto a template data map. Every key is always
// written so the template never has to distinguish "missing" from "empty".
func ApplyTemplateData(m map[string]any, head Head) {
	if m == nil {
		return
	}
	m["SEOTitle"] = head.Title
	m["SEODescription"] = head.Description
	m["SEOKeywords"] = head.Keywords
	m["SEOAuthor"] = head.Author
	m["SEOCanonical"] = head.Canonical
	m["SEOImage"] = head.Image
	m["SEOImageAlt"] = head.ImageAlt
	m["SEORobots"] = head.Robots
	m["SEOType"] = head.Type
	m["SEOLocale"] = head.Locale
	m["SEOJSONLD"] = head.JSONLD
}

// BuildTemplateData is a convenience wrapper: resolve the head for a page and
// merge it into an existing template data map.
func BuildTemplateData(m map[string]any, input Options) Head {
	head := Build(input)
	ApplyTemplateData(m, head)
	return head
}

// PageOptions builds the head inputs for one of the public navigation pages.
// The page name is localised with the same i18n keys the templates use, so the
// document title, breadcrumb and sitemap labels stay in sync.
func PageOptions(options model.Application, r *http.Request, kind PageKind) Options {
	locale := i18n.NormalizeLocale(options.Locale)
	switch kind {
	case PageApplications:
		return Options{
			Kind:    kind,
			Options: options,
			Request: r,
			Path:    define.RegularPages.Applications.Path,
			Name:    i18n.T(locale, "page_apps"),
		}
	case PageBookmarks:
		return Options{
			Kind:    kind,
			Options: options,
			Request: r,
			Path:    define.RegularPages.Bookmarks.Path,
			Name:    i18n.T(locale, "page_bookmarks"),
		}
	case PageHelp:
		return Options{
			Kind:    kind,
			Options: options,
			Request: r,
			Path:    define.RegularPages.Help.Path,
			Name:    i18n.T(locale, "page_help"),
		}
	default:
		return Options{
			Kind:    PageHome,
			Options: options,
			Request: r,
			Path:    define.RegularPages.Home.Path,
		}
	}
}
