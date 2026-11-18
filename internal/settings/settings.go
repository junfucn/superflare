package settings

import (
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/config/define"
)

func RegisterRouting(e *echo.Echo) {
	e.GET(define.RegularPages.Settings.Path, pageHome)
	e.GET(define.RegularPages.Settings.Path+"/", pageHome)
}

func RedirectAfterSave(c *echo.Context, path string) error {
	return c.Redirect(http.StatusSeeOther, path+"?saved=1")
}

func Render(c *echo.Context, code int, m map[string]any) error {
	m["SettingsSaved"] = code == http.StatusOK &&
		c.Request().Method == http.MethodGet && c.QueryParam("saved") == "1"
	return c.Render(code, "settings.html", m)
}

func pageHome(c *echo.Context) error {
	target := define.SettingPages.Theme.Path
	if rawQuery := c.QueryString(); rawQuery != "" {
		if parsed, err := url.Parse(target); err == nil {
			parsed.RawQuery = rawQuery
			target = parsed.String()
		}
	}
	return c.Redirect(http.StatusFound, target)
}
