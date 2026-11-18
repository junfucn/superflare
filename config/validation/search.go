package validation

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
)

var ErrInvalidSearchTemplate = errors.New("custom search engine template must be an absolute HTTP(S) URL containing a %s placeholder")

// BuildSearchURL keeps the destination fixed and encodes the search term for its URL component.
func BuildSearchURL(pattern, keyword string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	_, rest, hasScheme := strings.Cut(pattern, "://")
	if !hasScheme || !strings.Contains(pattern, "%s") ||
		strings.Contains(pattern, `\`) || strings.IndexFunc(pattern, unicode.IsSpace) >= 0 {
		return "", ErrInvalidSearchTemplate
	}
	authority := rest
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority = authority[:end]
	}
	if strings.Contains(authority, "%s") {
		return "", ErrInvalidSearchTemplate
	}
	pathAndQuery, fragment, hasFragment := strings.Cut(pattern, "#")
	path, query, hasQuery := strings.Cut(pathAndQuery, "?")
	queryKeyword := url.QueryEscape(keyword)
	pathKeyword := strings.ReplaceAll(queryKeyword, "+", "%20")
	target := strings.ReplaceAll(path, "%s", pathKeyword)
	if hasQuery {
		target += "?" + strings.ReplaceAll(query, "%s", queryKeyword)
	}
	if hasFragment {
		target += "#" + strings.ReplaceAll(fragment, "%s", pathKeyword)
	}
	u, err := url.Parse(target)
	if err != nil || !IsValidHTTPURL(target) {
		return "", ErrInvalidSearchTemplate
	}
	if _, err := url.QueryUnescape(u.RawQuery); err != nil {
		return "", ErrInvalidSearchTemplate
	}
	return target, nil
}
