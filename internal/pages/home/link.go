package home

import (
	"bytes"
	"html/template"
	"net/url"
	"strings"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
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

func renderNetworkAttributes(b *bytes.Buffer, source, local string, encrypted bool, requestURL *fn.DynamicURL) {
	if !isSafeBookmarkURL(source) || !shouldUseLocalRedirect(source, local, false, nil) {
		return
	}
	for _, attr := range [][2]string{
		{"data-network-local", local},
		{"data-network-source", renderBookmarkHref(source, "", false, encrypted, requestURL)},
		{"data-network-redirect", renderBookmarkHref(source, local, false, encrypted, requestURL)},
	} {
		b.WriteByte(' ')
		b.WriteString(attr[0])
		b.WriteString(`="`)
		b.WriteString(template.HTMLEscapeString(attr[1]))
		b.WriteByte('"')
	}
}
