package home

import (
	"html/template"
	"strings"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/internal/fn"
	"github.com/junfuchang/superflare/internal/resources/mdi"
)

var getSiteFaviconFast = fn.GetSiteFaviconFast
var getSiteFaviconAssetURL = fn.GetSiteFaviconAssetURL

func renderBookmarkIcon(icon string, link string, iconMode string) string {
	iconMode = strings.ToUpper(strings.TrimSpace(iconMode))
	if iconMode == define.IconModeHidden {
		return ""
	}
	defaultBookmarkIcon := mdi.GetIconByName("bookmark")

	icon = strings.TrimSpace(icon)
	var markup string
	if strings.HasPrefix(icon, "http://") || strings.HasPrefix(icon, "https://") {
		markup = `<img src="` + template.HTMLEscapeString(icon) + `" decoding="async" alt="">`
	} else if icon != "" {
		markup = mdi.GetIconByName(icon)
		if markup == "" {
			markup = iconFallbackForLink(link, defaultBookmarkIcon)
		}
	} else if iconMode == define.IconModeMissingFill {
		markup = iconFallbackForLink(link, defaultBookmarkIcon)
	}

	// Lazy images do not hold up the page load event, even inside the viewport.
	return strings.Replace(markup, "<img ", `<img loading="lazy" `, 1)
}

func iconFallbackForLink(link string, fallback string) string {
	if favicon := getSiteFaviconFast(link, ""); favicon != "" {
		return markDirectSiteFaviconForFallback(favicon, mdi.GetIconURLByName("bookmark"))
	}
	iconURL := strings.TrimSpace(getSiteFaviconAssetURL(link))
	if iconURL == "" {
		return fallback
	}
	return markFallbackIconForAsyncSiteFavicon(fallback, iconURL)
}

func markDirectSiteFaviconForFallback(favicon string, fallbackURL string) string {
	if favicon == "" || fallbackURL == "" || !strings.HasPrefix(favicon, "<img ") {
		return favicon
	}
	escaped := template.HTMLEscapeString(fallbackURL)
	return strings.Replace(favicon, "<img ", `<img data-site-icon-direct="1" data-site-icon-fallback-src="`+escaped+`" `, 1)
}

func markFallbackIconForAsyncSiteFavicon(fallback string, iconURL string) string {
	if fallback == "" || iconURL == "" {
		return fallback
	}
	escaped := template.HTMLEscapeString(iconURL)
	if strings.Contains(fallback, `data-site-icon-src=`) {
		return fallback
	}
	if strings.HasPrefix(fallback, "<img ") {
		return strings.Replace(fallback, "<img ", `<img data-site-icon-src="`+escaped+`" `, 1)
	}
	if strings.HasPrefix(fallback, "<svg ") {
		return strings.Replace(fallback, "<svg ", `<svg data-site-icon-src="`+escaped+`" `, 1)
	}
	return fallback
}
