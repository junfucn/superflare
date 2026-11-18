package redir

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/config/validation"
	"github.com/junfuchang/superflare/internal/auth"
	"github.com/junfuchang/superflare/internal/background"
	"github.com/junfuchang/superflare/internal/fn"
	"github.com/junfuchang/superflare/internal/localnetwork"
	"github.com/junfuchang/superflare/internal/statuspage"
)

func RegisterRouting(e *echo.Echo) {
	e.GET(define.MiscPages.RedirHome.Path, func(c *echo.Context) error {
		return c.Redirect(http.StatusFound, define.RegularPages.Home.Path)
	})

	e.GET(define.MiscPages.RedirHelper.Path, func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		encoded := c.QueryParam("go")
		if len(encoded) < 1 {
			return renderRedirectInvalidTarget(c)
		}
		decoded, err := data.Base64DecodeUrl(encoded)
		if err != nil {
			return renderRedirectInvalidTarget(c)
		}
		decodeURL := string(decoded)
		if !validation.IsSafeNavigationURL(decodeURL) {
			return renderRedirectInvalidTarget(c)
		}
		requestURL, err := redirectRequestURL(c.Request())
		if err != nil {
			return renderRedirectInvalidTarget(c)
		}
		appsData, errApps := data.LoadFavoriteBookmarks()
		if errApps != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, errApps.Error()))
		}
		canViewPrivate := auth.IsLoginDisabled(c) || auth.ResolveLoginDisplayStateForView(c).ShowLoginInfo
		for _, bookmark := range appsData.Items {
			if bookmark.Private && !canViewPrivate {
				continue
			}
			if fn.ParseDynamicUrlWith(bookmark.URL, &requestURL) == decodeURL {
				return c.Redirect(http.StatusFound, string(decoded))
			}
		}
		bookmarksData, errBookmarks := data.LoadNormalBookmarks()
		if errBookmarks != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, errBookmarks.Error()))
		}
		for _, bookmark := range bookmarksData.Items {
			if bookmark.Private && !canViewPrivate {
				continue
			}
			if fn.ParseDynamicUrlWith(bookmark.URL, &requestURL) == decodeURL {
				return c.Redirect(http.StatusFound, string(decoded))
			}
		}
		return renderRedirectInvalidTarget(c)
	})

	e.GET(define.MiscPages.RedirLocal.Path, func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		sourceURL, errSource := decodeRedirectParam(c.QueryParam("go"))
		localURL, errLocal := decodeRedirectParam(c.QueryParam("local"))
		if errSource != nil || errLocal != nil || !isHTTPRedirectURL(sourceURL) || !isHTTPRedirectURL(localURL) {
			return renderRedirectInvalidTarget(c)
		}
		requestURL, err := redirectRequestURL(c.Request())
		if err != nil {
			return renderRedirectInvalidTarget(c)
		}
		canViewPrivate := auth.IsLoginDisabled(c) || auth.ResolveLoginDisplayStateForView(c).ShowLoginInfo
		pairExists, err := bookmarkLocalURLPairExists(&requestURL, sourceURL, localURL, canViewPrivate)
		if err != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
		}
		if !pairExists {
			return renderRedirectInvalidTarget(c)
		}
		options, err := data.GetAllSettingsOptions()
		if err != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
		}
		statuspage.BindOptions(c, options)
		locale := options.Locale
		bodyStyle, styleWarning, err := statuspage.RequireConfiguredBodyStyleForRender(locale, "")
		if err != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(locale, http.StatusInternalServerError, err.Error()))
		}
		var renderWarnings []string
		if styleWarning != "" {
			renderWarnings = append(renderWarnings, styleWarning)
		}
		page, err := renderLocalRedirectPage(sourceURL, localURL, options, bodyStyle, renderWarnings, c.Request())
		if err != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(locale, http.StatusInternalServerError, err.Error()))
		}
		return c.HTMLBlob(http.StatusOK, page)
	})
}

func renderRedirectInvalidTarget(c *echo.Context) error {
	if err := statuspage.BindCurrentOptions(c); err != nil {
		if strings.Contains(err.Error(), "is missing") {
			return statuspage.HTML(c, http.StatusBadRequest, statuspage.BuildRedirectInvalidTargetPage(statuspage.CurrentLocale(c)))
		}
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
	}
	return statuspage.HTML(c, http.StatusBadRequest, statuspage.BuildRedirectInvalidTargetPage(statuspage.CurrentLocale(c)))
}

func decodeRedirectParam(encoded string) (string, error) {
	if strings.TrimSpace(encoded) == "" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "missing redirect target")
	}
	decoded, err := data.Base64DecodeUrl(encoded)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(decoded)), nil
}

func isHTTPRedirectURL(rawURL string) bool {
	return localnetwork.IsHTTPURL(rawURL)
}

func redirectRequestURL(r *http.Request) (fn.DynamicURL, error) {
	requestURL := fn.ParseRequestURLTo(r)
	pages, supplied := r.URL.Query()["from"]
	if !supplied {
		return requestURL, nil
	}
	if len(pages) != 1 || strings.ContainsAny(pages[0], "#\r\n\x00") {
		return requestURL, fmt.Errorf("invalid redirect page context")
	}
	page, err := url.ParseRequestURI(pages[0])
	if err != nil || page.IsAbs() || page.Host != "" {
		return requestURL, fmt.Errorf("invalid redirect page context")
	}
	switch page.Path {
	case define.RegularPages.Home.Path, define.RegularPages.Applications.Path, define.RegularPages.Bookmarks.Path:
		// Only the page URI is supplied; origin still follows the request's proxy trust policy.
		requestURL.Pathname = page.Path
		requestURL.Href = requestURL.Origin + page.RequestURI()
		return requestURL, nil
	default:
		return requestURL, fmt.Errorf("invalid redirect page context")
	}
}

func bookmarkLocalURLPairExists(requestURL *fn.DynamicURL, sourceURL string, localURL string, canViewPrivate bool) (bool, error) {
	appsData, errApps := data.LoadFavoriteBookmarks()
	if errApps != nil {
		return false, errApps
	}
	if bookmarksContainLocalPair(appsData.Items, requestURL, sourceURL, localURL, canViewPrivate) {
		return true, nil
	}
	bookmarksData, errBookmarks := data.LoadNormalBookmarks()
	if errBookmarks != nil {
		return false, errBookmarks
	}
	return bookmarksContainLocalPair(bookmarksData.Items, requestURL, sourceURL, localURL, canViewPrivate), nil
}

func bookmarksContainLocalPair(bookmarks []model.Bookmark, requestURL *fn.DynamicURL, sourceURL string, localURL string, canViewPrivate bool) bool {
	for _, bookmark := range bookmarks {
		if bookmark.Private && !canViewPrivate {
			continue
		}
		if fn.ParseDynamicUrlWith(bookmark.URL, requestURL) == sourceURL && fn.ParseDynamicUrlWith(bookmark.LocalURL, requestURL) == localURL {
			return true
		}
	}
	return false
}

//go:embed local.html
var localPageSource string

var localPageTemplate = template.Must(template.New("local-redirect").Parse(localPageSource))

func renderLocalRedirectPage(sourceURL string, localURL string, options model.Application, bodyStyle template.CSS, warnings []string, requests ...*http.Request) ([]byte, error) {
	if _, err := marshalRedirectJSValue("source url", sourceURL); err != nil {
		return nil, err
	}
	if _, err := marshalRedirectJSValue("local url", localURL); err != nil {
		return nil, err
	}
	var request *http.Request
	if len(requests) > 0 {
		request = requests[0]
	}
	config := localnetwork.Config(options, request)
	config.Source, config.Local, config.Redirect = sourceURL, localURL, true
	assets := background.ResolveAssets(options)
	var output bytes.Buffer
	err := localPageTemplate.Execute(&output, map[string]any{
		"English": strings.EqualFold(options.Locale, "en"),
		"Source":  sourceURL, "Local": localURL, "Style": bodyStyle,
		"NetworkStyle": localnetwork.Style(), "Script": localnetwork.Script(config),
		"Nonce": rand.Text(), "Warnings": warnings, "Background": assets,
		"BackgroundScript": template.JS(background.InlineLoaderScript),
		"BackgroundStyle":  template.CSS(fmt.Sprintf("opacity:%.2f;filter:blur(%dpx)", float64(min(100, max(0, options.BackgroundOpacity)))/100, min(80, max(0, options.BackgroundBlur)))),
	})
	return output.Bytes(), err
}

func marshalRedirectJSValue(label string, value string) ([]byte, error) {
	if !utf8.ValidString(value) {
		return nil, fmt.Errorf("marshal redirect %s failed: input contains invalid UTF-8", label)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal redirect %s failed: %w", label, err)
	}
	return raw, nil
}
