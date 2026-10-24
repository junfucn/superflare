package seo

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/auth"
	"github.com/junfuchang/superflare/internal/footer"
	"github.com/junfuchang/superflare/internal/pool"
	settingsroot "github.com/junfuchang/superflare/internal/settings"
	"github.com/junfuchang/superflare/internal/statuspage"
)

func RegisterRouting(e *echo.Echo) {
	e.GET(define.SettingPages.SEO.Path, pageSEO, auth.AuthRequired)
	e.POST(define.SettingPages.SEO.Path, updateSEOOptions, auth.AuthRequired)
}

func updateSEOOptions(c *echo.Context) error {
	var body struct {
		SiteURL        string `form:"site-url"`
		SEODescription string `form:"seo-description"`
		SEOKeywords    string `form:"seo-keywords"`
		SEOImage       string `form:"seo-image"`
		SEOAuthor      string `form:"seo-author"`
		SEONoIndex     string `form:"seo-noindex"`
	}
	if err := c.Bind(&body); err != nil {
		return statuspage.HTML(c, http.StatusBadRequest, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusBadRequest, "missing form data"))
	}
	var update model.Application
	update.SiteURL = strings.TrimSpace(body.SiteURL)
	update.SEODescription = strings.TrimSpace(body.SEODescription)
	update.SEOKeywords = strings.TrimSpace(body.SEOKeywords)
	update.SEOImage = strings.TrimSpace(body.SEOImage)
	update.SEOAuthor = strings.TrimSpace(body.SEOAuthor)
	update.SEONoIndex = strings.TrimSpace(body.SEONoIndex) == "1"
	if err := data.UpdateSEO(update); err != nil {
		return statuspage.HTML(c, http.StatusBadRequest, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusBadRequest, err.Error()))
	}
	return pageSEO(c)
}

func pageSEO(c *echo.Context) error {
	options, err := data.GetAllSettingsOptions()
	if err != nil {
		statuspage.BindOptionsLoadError(c, err)
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
	}
	statuspage.BindOptions(c, options)
	options, renderWarnings := statuspage.PrepareSettingsOptionsForRender(options)
	locale := options.Locale
	showLoginInfo := false
	userName := ""
	loginDate := ""
	loginDisplay, err := auth.ResolveLoginDisplayStateForStrictView(c)
	if err != nil {
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(locale, http.StatusInternalServerError, err.Error()))
	}
	showLoginInfo = loginDisplay.ShowLoginInfo
	userName = loginDisplay.UserName
	loginDate = loginDisplay.LoginDate
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
	m["PageName"] = "SEO"
	m["PageAppearance"] = pageStyle
	m["SettingPages"] = define.SettingPages
	m["ShowSettingsSidebar"] = true
	m["ShowPortsSettings"] = !auth.IsLoginDisabled(c)
	m["DisableLoginMode"] = auth.IsLoginDisabled(c)
	m["SettingsURI"] = define.RegularPages.Settings.Path
	m["ShowLoginInfo"] = showLoginInfo
	m["UserIsLogin"] = showLoginInfo
	m["UserName"] = userName
	m["LoginDate"] = loginDate
	m["OptionSiteURL"] = options.SiteURL
	m["OptionSEODescription"] = options.SEODescription
	m["OptionSEOKeywords"] = options.SEOKeywords
	m["OptionSEOImage"] = options.SEOImage
	m["OptionSEOAuthor"] = options.SEOAuthor
	m["OptionSEONoIndex"] = options.SEONoIndex
	m["OptionTitle"] = options.Title
	m["OptionSiteIcon"] = options.SiteIcon
	footer.BindTemplateData(m, options.Footer)
	m["RenderWarnings"] = renderWarnings
	return c.Render(http.StatusOK, "settings.html", m)
}
