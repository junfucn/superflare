package home

import (
	"bytes"
	"html/template"
	"net/url"
	"strings"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/config/validation"
	"github.com/junfuchang/superflare/internal/fn"
	"github.com/junfuchang/superflare/internal/localnetwork"
)

func renderBookmarkHref(sourceURL string, localURL string, preferLocal bool, enableEncryptedLink bool, requestURL *fn.DynamicURL) string {
	if !isSafeBookmarkURL(sourceURL) {
		return ""
	}
	if shouldUseLocalRedirect(sourceURL, localURL, preferLocal, requestURL) {
		return define.MiscPages.RedirLocal.Path + "?go=" + data.Base64EncodeUrl(sourceURL) + "&local=" + data.Base64EncodeUrl(localURL) + redirectPageContext(requestURL)
	}
	if strings.HasPrefix(strings.ToLower(sourceURL), "chrome-extension://") || enableEncryptedLink {
		return define.MiscPages.RedirHelper.Path + "?go=" + data.Base64EncodeUrl(sourceURL) + redirectPageContext(requestURL)
	}
	return sourceURL
}

func isSafeBookmarkURL(rawURL string) bool {
	return validation.IsSafeNavigationURL(rawURL)
}

func renderBookmarkNavigationAttributes(b *bytes.Buffer, href string, openInNewTab bool) {
	if openInNewTab && href != "" {
		b.WriteString(` target="_blank"`)
	}
	b.WriteString(` rel="noopener"`)
	if href == "" {
		b.WriteString(` tabindex="0" role="link" aria-disabled="true"`)
		return
	}
	b.WriteString(` href="`)
	b.WriteString(template.HTMLEscapeString(href))
	b.WriteByte('"')
}

func redirectPageContext(requestURL *fn.DynamicURL) string {
	if requestURL == nil || requestURL.Origin == "" {
		return ""
	}
	requestURI, ok := strings.CutPrefix(requestURL.Href, requestURL.Origin)
	if !ok || requestURI == "" {
		return ""
	}
	return "&from=" + url.QueryEscape(requestURI)
}

func shouldUseLocalRedirect(sourceURL string, localURL string, preferLocal bool, requestURL *fn.DynamicURL) bool {
	return isHTTPBookmarkURL(sourceURL) && isHTTPBookmarkURL(localURL) && sourceURL != localURL
}

func isHTTPBookmarkURL(rawURL string) bool {
	return localnetwork.IsHTTPURL(rawURL)
}

func renderBookmarkLinkAttributes(b *bytes.Buffer, item model.Bookmark, openInNewTab bool, options *model.Application, preferLocal bool, requestURL *fn.DynamicURL) {
	sourceHref := renderBookmarkHref(item.URL, "", false, options.EnableEncryptedLink, requestURL)
	hasLocal := sourceHref != "" && shouldUseLocalRedirect(item.URL, item.LocalURL, preferLocal, requestURL)
	href := sourceHref
	if hasLocal && !options.LocalNetworkDirectFallback {
		href = renderBookmarkHref(item.URL, item.LocalURL, preferLocal, options.EnableEncryptedLink, requestURL)
	}
	renderBookmarkNavigationAttributes(b, href, openInNewTab)
	if !hasLocal {
		return
	}
	b.WriteString(` data-network-local="`)
	b.WriteString(template.HTMLEscapeString(item.LocalURL))
	b.WriteString(`" data-network-source="`)
	b.WriteString(template.HTMLEscapeString(sourceHref))
	b.WriteByte('"')
	if !options.LocalNetworkDirectFallback {
		b.WriteString(` data-network-redirect="`)
		b.WriteString(template.HTMLEscapeString(href))
		b.WriteByte('"')
	}
}
