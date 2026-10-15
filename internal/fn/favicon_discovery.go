package fn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	xhtml "golang.org/x/net/html"
)

const siteIconCandidateLimit = 8

type siteFaviconPageMetadata struct {
	hrefs        []string
	baseHref     string
	moduleScript bool
}

func discoverSiteFaviconFromHTML(ctx context.Context, rootIconURL string) ([]string, error) {
	base, err := url.Parse(strings.TrimSpace(rootIconURL))
	if err != nil || base == nil || base.Hostname() == "" {
		return nil, fmt.Errorf("invalid favicon URL")
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("unsupported page URL scheme")
	}
	base.Path = "/"
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", siteIconUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := siteIconHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected page status: %d", resp.StatusCode)
	}

	metadata, err := collectSiteFaviconPageMetadata(io.LimitReader(resp.Body, siteIconHTMLBytes))
	if err != nil {
		return nil, err
	}
	pageURL := base
	if resp.Request != nil && resp.Request.URL != nil {
		pageURL = resp.Request.URL
	}
	iconBase := pageURL
	if resolved := resolveDiscoveredSiteFavicon(pageURL, metadata.baseHref); resolved != "" {
		if candidate, err := url.Parse(resolved); err == nil && (candidate.Scheme == "http" || candidate.Scheme == "https") {
			iconBase = candidate
		}
	}
	var candidates []string
	seen := make(map[string]bool)
	for _, href := range metadata.hrefs {
		resolved := resolveDiscoveredSiteFavicon(iconBase, href)
		if resolved != "" && !seen[resolved] {
			seen[resolved] = true
			candidates = append(candidates, resolved)
			if len(candidates) == siteIconCandidateLimit {
				break
			}
		}
	}
	if len(candidates) != 0 {
		return candidates, nil
	}
	if metadata.moduleScript {
		iconURL, err := discoverSPASiteFavicon(ctx, pageURL)
		if err != nil {
			return nil, err
		}
		return []string{iconURL}, nil
	}
	return nil, fmt.Errorf("no html favicon found")
}

func resolveDiscoveredSiteFavicon(base *url.URL, href string) string {
	if strings.TrimSpace(href) == "" {
		return ""
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return ""
	}
	if ref.Scheme == "data" {
		ref.Fragment = ""
		return ref.String()
	}
	resolved := base.ResolveReference(ref)
	if (resolved.Scheme != "http" && resolved.Scheme != "https") || !isProxyableSiteFaviconURL(resolved.String()) {
		return ""
	}
	resolved.Fragment = ""
	return resolved.String()
}

func discoverSPASiteFavicon(ctx context.Context, pageURL *url.URL) (string, error) {
	// Dujiao-Next and compatible SPAs publish their dynamic head metadata here.
	configURL := pageURL.ResolveReference(&url.URL{Path: "/api/v1/public/config"})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", siteIconUserAgent)
	resp, err := siteIconHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected public config status: %d", resp.StatusCode)
	}
	var config struct {
		Data struct {
			Brand *struct {
				SiteIcon string `json:"site_icon"`
			} `json:"brand"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, siteIconHTMLBytes)).Decode(&config); err != nil {
		return "", fmt.Errorf("decode public site config: %w", err)
	}
	if config.Data.Brand == nil {
		return "", fmt.Errorf("no public config brand found")
	}
	href := strings.TrimSpace(config.Data.Brand.SiteIcon)
	if href == "" {
		// Match Dujiao-Next's frontend fallback when no custom icon is set.
		href = "/dj.svg"
	}
	iconURL := resolveDiscoveredSiteFavicon(pageURL, href)
	if iconURL == "" {
		return "", fmt.Errorf("no public config favicon found")
	}
	return iconURL, nil
}

func collectFaviconHrefs(reader io.Reader) ([]string, error) {
	metadata, err := collectSiteFaviconPageMetadata(reader)
	return metadata.hrefs, err
}

func collectSiteFaviconPageMetadata(reader io.Reader) (siteFaviconPageMetadata, error) {
	tokenizer := xhtml.NewTokenizer(reader)
	var metadata siteFaviconPageMetadata
	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			err := tokenizer.Err()
			if err != nil && !errors.Is(err, io.EOF) {
				return metadata, err
			}
			return metadata, nil
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			token := tokenizer.Token()
			switch strings.ToLower(token.Data) {
			case "link":
				if href, ok := faviconHrefFromAttributes(token.Attr); ok {
					metadata.hrefs = append(metadata.hrefs, href)
				}
			case "base":
				for _, attr := range token.Attr {
					if attr.Key == "href" && metadata.baseHref == "" {
						metadata.baseHref = attr.Val
					}
				}
			case "script":
				for _, attr := range token.Attr {
					if attr.Key == "type" && strings.EqualFold(strings.TrimSpace(attr.Val), "module") {
						metadata.moduleScript = true
					}
				}
			}
		case xhtml.EndTagToken:
			if strings.EqualFold(tokenizer.Token().Data, "head") {
				return metadata, nil
			}
		}
	}
}

func faviconHrefFromAttributes(attributes []xhtml.Attribute) (string, bool) {
	var rel, href string
	for _, attr := range attributes {
		switch strings.ToLower(strings.TrimSpace(attr.Key)) {
		case "rel":
			rel = attr.Val
		case "href":
			href = strings.TrimSpace(attr.Val)
		}
	}
	return href, href != "" && relLooksLikeFavicon(rel)
}

func relLooksLikeFavicon(rel string) bool {
	for _, token := range strings.Fields(strings.ReplaceAll(strings.ToLower(rel), ",", " ")) {
		if token == "icon" || strings.Contains(token, "-icon") {
			return true
		}
	}
	return false
}
