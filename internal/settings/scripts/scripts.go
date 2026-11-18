package scripts

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/internal/auth"
	"github.com/junfuchang/superflare/internal/footer"
	"github.com/junfuchang/superflare/internal/pool"
	settingsroot "github.com/junfuchang/superflare/internal/settings"
	"github.com/junfuchang/superflare/internal/statuspage"
)

func RegisterRouting(e *echo.Echo) {
	e.GET(define.SettingPages.Scripts.Path, pageScripts, auth.AuthRequired)
	e.POST(define.SettingPages.Scripts.Path, updateScripts, auth.AuthRequired)
}

func updateScripts(c *echo.Context) error {
	var body struct {
		CustomScripts string `form:"custom-scripts"`
	}
	if err := c.Bind(&body); err != nil {
		return statuspage.HTML(c, http.StatusBadRequest, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusBadRequest, "missing form data"))
	}
	if err := data.UpdateCustomScripts(body.CustomScripts); err != nil {
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
	}
	return settingsroot.RedirectAfterSave(c, define.SettingPages.Scripts.Path)
}

func pageScripts(c *echo.Context) error {
	options, err := data.GetAllSettingsOptions()
	if err != nil {
		statuspage.BindOptionsLoadError(c, err)
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
	}
	statuspage.BindOptions(c, options)
	options, renderWarnings := statuspage.PrepareSettingsOptionsForRender(options)
	locale := options.Locale
	loginDisplay, err := auth.ResolveLoginDisplayStateForStrictView(c)
	if err != nil {
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(locale, http.StatusInternalServerError, err.Error()))
	}
	renderWarnings = auth.AppendSessionWarnings(c, locale, renderWarnings)
	pageStyle, styleWarning, err := statuspage.RequireConfiguredBodyStyleForRender(locale, "settings")
	if err != nil {
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(locale, http.StatusInternalServerError, err.Error()))
	}
	if styleWarning != "" {
		renderWarnings = append(renderWarnings, styleWarning)
	}

	m := pool.GetTemplateMap()
	defer pool.PutTemplateMap(m)
	m["Locale"] = locale
	m["DebugMode"] = settingsroot.CurrentRuntime().DebugMode
	m["PageInlineStyle"] = define.GetPageInlineStyle()
	m["PageName"] = "Scripts"
	m["PageAppearance"] = pageStyle
	m["SettingPages"] = define.SettingPages
	m["ShowSettingsSidebar"] = true
	m["ShowPortsSettings"] = !auth.IsLoginDisabled(c)
	m["DisableLoginMode"] = auth.IsLoginDisabled(c)
	m["SettingsURI"] = define.RegularPages.Settings.Path
	m["ShowLoginInfo"] = loginDisplay.ShowLoginInfo
	m["UserIsLogin"] = loginDisplay.ShowLoginInfo
	m["UserName"] = loginDisplay.UserName
	m["LoginDate"] = loginDisplay.LoginDate
	m["OptionTitle"] = options.Title
	m["OptionSiteIcon"] = options.SiteIcon
	m["OptionCustomScripts"] = options.CustomScripts
	m["RenderWarnings"] = renderWarnings
	footer.BindTemplateData(m, options.Footer)
	return settingsroot.Render(c, http.StatusOK, m)
}
