package seo

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/junfuchang/superflare/config/model"
)

const (
	defaultScheme = "http"
	schemeHTTP    = "http"
	schemeHTTPS   = "https"
)

// ResolveOrigin returns the absolute site origin ("https://example.com") used
// for canonical URLs, Open Graph tags and the machine-readable endpoints.
//
// A configured SiteURL always wins: it is the only way to produce stable URLs
// behind a reverse proxy that rewrites Host/proto. When it is empty the origin
// is derived from the incoming request, honouring X-Forwarded-Proto/Host so
// that deployments behind a proxy still emit https links.
func ResolveOrigin(options model.Application, r *http.Request) string {
	if site := normalizeSiteURL(options.SiteURL); site != "" {
		return site
	}
	if r == nil {
		return ""
	}
	scheme := requestScheme(r)
	host := requestHost(r)
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// NormalizeSiteURL validates and canonicalises a configured site URL. It
// returns "" when the value cannot be used as an origin.
func NormalizeSiteURL(raw string) string {
	return normalizeSiteURL(raw)
}

func normalizeSiteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != schemeHTTP && scheme != schemeHTTPS {
		return ""
	}
	base := scheme + "://" + parsed.Host
	path := strings.TrimRight(parsed.Path, "/")
	if path != "" && path != "/" {
		base += path
	}
	return base
}

func requestScheme(r *http.Request) string {
	if r == nil {
		return defaultScheme
	}
	if forwarded := headerFirstValue(r.Header.Get("X-Forwarded-Proto")); forwarded == schemeHTTP || forwarded == schemeHTTPS {
		return forwarded
	}
	if forwarded := forwardedElement(r.Header.Get("Forwarded"), "proto"); forwarded == schemeHTTP || forwarded == schemeHTTPS {
		return forwarded
	}
	if r.TLS != nil {
		return schemeHTTPS
	}
	return defaultScheme
}

func requestHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := headerFirstValue(r.Header.Get("X-Forwarded-Host")); isSafeHost(forwarded) {
		return forwarded
	}
	host := strings.TrimSpace(r.Host)
	if isSafeHost(host) {
		return host
	}
	return ""
}

func headerFirstValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if index := strings.IndexByte(raw, ','); index >= 0 {
		raw = raw[:index]
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

func forwardedElement(raw string, key string) string {
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		name, value, ok := strings.Cut(part, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), key) {
			continue
		}
		return strings.ToLower(strings.Trim(strings.TrimSpace(value), `"`))
	}
	return ""
}

// isSafeHost accepts host[:port] shaped values only. This keeps untrusted
// header content out of canonical URLs and the XML sitemap.
func isSafeHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || len(host) > 255 {
		return false
	}
	for _, char := range host {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '.', char == '-', char == ':', char == '[', char == ']':
		default:
			return false
		}
	}
	return true
}

// AbsoluteURL resolves a possibly site-relative path against the origin.
func AbsoluteURL(origin, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if parsed, err := url.Parse(path); err == nil && parsed.IsAbs() {
		return path
	}
	if origin == "" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(origin, "/") + path
}

func joinPath(origin, path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(origin, "/") + path
}
