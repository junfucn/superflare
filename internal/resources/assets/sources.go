package assets

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/internal/auth"
	"github.com/junfuchang/superflare/internal/fn"
)

// Only operator-configured sources may cause network requests. Do not expand
// request-derived URL placeholders here: Host and forwarded headers are untrusted.
func allowedSiteIconSources(c *echo.Context) (map[string]struct{}, error) {
	apps, err := data.LoadFavoriteBookmarks()
	if err != nil {
		return nil, err
	}
	bookmarks, err := data.LoadNormalBookmarks()
	if err != nil {
		return nil, err
	}
	canViewPrivate := auth.IsLoginDisabled(c) || auth.ResolveLoginDisplayStateForView(c).ShowLoginInfo
	sources := make(map[string]struct{})
	for _, item := range append(apps.Items, bookmarks.Items...) {
		if item.Private && !canViewPrivate {
			continue
		}
		if source := fn.GetSiteFaviconURL(item.URL); source != "" {
			sources[source] = struct{}{}
		}
		if source := strings.TrimSpace(item.Icon); strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
			sources[source] = struct{}{}
		}
	}
	return sources, nil
}

func requireConfiguredBackground(c *echo.Context, source string) error {
	options, err := data.GetAllSettingsOptions()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "background configuration unavailable")
	}
	if strings.EqualFold(options.BackgroundImageMode, "upload") || source != strings.TrimSpace(options.BackgroundImage) {
		return echo.NewHTTPError(http.StatusForbidden, "background source is not configured")
	}
	return nil
}

func protectImageResponse(c *echo.Context) {
	c.Response().Header().Set("Content-Security-Policy", siteIconContentSecurityPolicy)
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
}
