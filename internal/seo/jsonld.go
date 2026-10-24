package seo

import (
	"encoding/json"
	"html/template"
	"strings"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/internal/i18n"
)

const (
	fallbackSiteName = "SuperFlare"
	separator        = " · "
	robotsIndex      = "index, follow, max-image-preview:large, max-snippet:-1, max-video-preview:-1"
	robotsNoIndex    = "noindex, nofollow"
	pagesHomePath    = "/"
	schemaOrg        = "https://schema.org"
	// softwareApplicationCategory is the schema.org category used for the
	// self-hosted web application entry.
	softwareApplicationCategory = "WebApplication"
)

// translation returns an i18n string, or "" when the key is missing. i18n.T
// echoes the key back when it cannot resolve it, so that is used as the signal.
func translation(locale, key string) string {
	value := strings.TrimSpace(i18n.T(locale, key))
	if value == "" || value == key {
		return ""
	}
	return value
}

// copyFor resolves operator-visible copy for a locale: admin-configured text
// wins elsewhere, then the i18n bundle, then the built-in defaults.
func copyFor(locale, key string) string {
	if value := translation(locale, key); value != "" {
		return value
	}
	return builtinTranslation(i18n.NormalizeLocale(locale), key)
}

// Link is one curated outbound entry listed in structured data and llms.txt.
type Link struct {
	Name        string
	URL         string
	Description string
}

type jsonLDGraph struct {
	Context string `json:"@context"`
	Graph   []any  `json:"@graph"`
}

type jsonLDWebSite struct {
	Type        string     `json:"@type"`
	ID          string     `json:"@id"`
	URL         string     `json:"url"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	InLanguage  string     `json:"inLanguage,omitempty"`
	Publisher   *jsonLDRef `json:"publisher,omitempty"`
}

type jsonLDOrganization struct {
	Type        string     `json:"@type"`
	ID          string     `json:"@id"`
	URL         string     `json:"url,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Logo        *jsonLDImg `json:"logo,omitempty"`
}

type jsonLDSoftware struct {
	Type                string       `json:"@type"`
	ID                  string       `json:"@id"`
	Name                string       `json:"name"`
	Description         string       `json:"description,omitempty"`
	URL                 string       `json:"url,omitempty"`
	ApplicationCategory string       `json:"applicationCategory,omitempty"`
	OperatingSystem     string       `json:"operatingSystem,omitempty"`
	IsAccessibleForFree bool         `json:"isAccessibleForFree,omitempty"`
	Offers              *jsonLDPrice `json:"offers,omitempty"`
}

type jsonLDPrice struct {
	Type  string `json:"@type"`
	Price string `json:"price"`
}

type jsonLDWebPage struct {
	Type        string           `json:"@type"`
	ID          string           `json:"@id"`
	URL         string           `json:"url"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	InLanguage  string           `json:"inLanguage,omitempty"`
	IsPartOf    *jsonLDRef       `json:"isPartOf,omitempty"`
	Breadcrumb  *jsonLDCrumbList `json:"breadcrumb,omitempty"`
}

type jsonLDCrumbList struct {
	Type  string            `json:"@type"`
	Items []jsonLDCrumbItem `json:"itemListElement"`
}

type jsonLDCrumbItem struct {
	Type     string `json:"@type"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Item     string `json:"item,omitempty"`
}

type jsonLDRef struct {
	ID string `json:"@id"`
}

type jsonLDImg struct {
	Type string `json:"@type"`
	URL  string `json:"url"`
}

type jsonLDItemList struct {
	Type      string           `json:"@type"`
	ID        string           `json:"@id,omitempty"`
	Name      string           `json:"name,omitempty"`
	ItemCount int              `json:"numberOfItems"`
	Items     []jsonLDListItem `json:"itemListElement"`
}

type jsonLDListItem struct {
	Type     string          `json:"@type"`
	Position int             `json:"position"`
	Item     jsonLDItemValue `json:"item"`
}

type jsonLDItemValue struct {
	Type        string `json:"@type"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// buildJSONLD assembles the schema.org graph for a page. The graph always
// carries the WebSite, Organization and SoftwareApplication entities, plus a
// WebPage/BreadcrumbList for subpages and an ItemList for enumerated items.
// This is what lets LLM crawlers (and classic rich-result parsers) understand
// the site without executing JavaScript.
func buildJSONLD(input Options, head Head, origin string, locale string) template.JS {
	if origin == "" {
		return ""
	}
	siteName := strings.TrimSpace(input.Options.Title)
	if siteName == "" {
		siteName = fallbackSiteName
	}
	lang := "zh-CN"
	if i18n.NormalizeLocale(locale) == "en" {
		lang = "en"
	}
	homeURL := joinPath(origin, define.RegularPages.Home.Path)

	graph := make([]any, 0, 5)
	orgID := joinPath(origin, "/#organization")
	webSiteID := joinPath(origin, "/#website")

	graph = append(graph, jsonLDOrganization{
		Type:        "Organization",
		ID:          orgID,
		URL:         homeURL,
		Name:        siteName,
		Description: head.Description,
		Logo:        &jsonLDImg{Type: "ImageObject", URL: joinPath(origin, "/apple-touch-icon.png")},
	})
	graph = append(graph, jsonLDWebSite{
		Type:        "WebSite",
		ID:          webSiteID,
		URL:         homeURL,
		Name:        siteName,
		Description: head.Description,
		InLanguage:  lang,
		Publisher:   &jsonLDRef{ID: orgID},
	})
	graph = append(graph, jsonLDSoftware{
		Type:                "SoftwareApplication",
		ID:                  joinPath(origin, "/#app"),
		Name:                siteName,
		Description:         head.Description,
		URL:                 homeURL,
		ApplicationCategory: softwareApplicationCategory,
		OperatingSystem:     "Any",
		IsAccessibleForFree: true,
		Offers:              &jsonLDPrice{Type: "Offer", Price: "0"},
	})

	page := strings.TrimSpace(input.Path)
	if page == "" {
		page = pagesHomePath
	}
	if page != pagesHomePath {
		pageURL := joinPath(origin, page)
		webPage := jsonLDWebPage{
			Type:        "WebPage",
			ID:          pageURL + "#webpage",
			URL:         pageURL,
			Name:        head.Title,
			Description: head.Description,
			InLanguage:  lang,
			IsPartOf:    &jsonLDRef{ID: webSiteID},
		}
		if name := strings.TrimSpace(input.Name); name != "" {
			webPage.Breadcrumb = &jsonLDCrumbList{
				Type: "BreadcrumbList",
				Items: []jsonLDCrumbItem{
					{Type: "ListItem", Position: 1, Name: siteName, Item: homeURL},
					{Type: "ListItem", Position: 2, Name: name, Item: pageURL},
				},
			}
		}
		graph = append(graph, webPage)
	}

	if list := buildItemList(input, head, origin, page); list != nil {
		graph = append(graph, list)
	}

	payload := jsonLDGraph{Context: schemaOrg, Graph: graph}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	// A raw "<" inside a JSON-LD script block would terminate the tag early.
	return template.JS(strings.ReplaceAll(string(raw), "<", `\u003c`))
}

// buildItemList turns the page's enumerated links into an ItemList. This is how
// AI assistants learn what an applications/bookmarks page actually contains.
func buildItemList(input Options, head Head, origin string, page string) *jsonLDItemList {
	if len(input.Items) == 0 {
		return nil
	}
	list := &jsonLDItemList{
		Type:  "ItemList",
		ID:    joinPath(origin, page) + "#items",
		Name:  head.Title,
		Items: make([]jsonLDListItem, 0, len(input.Items)),
	}
	for _, item := range input.Items {
		url := strings.TrimSpace(item.URL)
		name := sanitizeJSONText(item.Name)
		if url == "" || name == "" {
			continue
		}
		list.Items = append(list.Items, jsonLDListItem{
			Type:     "ListItem",
			Position: len(list.Items) + 1,
			Item: jsonLDItemValue{
				Type:        "WebSite",
				URL:         url,
				Name:        name,
				Description: sanitizeJSONText(item.Description),
			},
		})
	}
	if len(list.Items) == 0 {
		return nil
	}
	list.ItemCount = len(list.Items)
	return list
}

// sanitizeJSONText collapses whitespace so operator text cannot inject control
// characters into the JSON-LD payload.
func sanitizeJSONText(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}
