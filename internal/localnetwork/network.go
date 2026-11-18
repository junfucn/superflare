package localnetwork

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/config/validation"
	"github.com/junfuchang/superflare/internal/fn"
	"github.com/labstack/echo/v5"
)

const IdentityPath = "/api/local-network/identity"

//go:embed client.js
var clientScript string

//go:embed controls.css
var controlsCSS string

type ClientConfig struct {
	URL      string `json:"url"`
	ID       string `json:"id"`
	Locale   string `json:"locale"`
	Source   string `json:"source,omitempty"`
	Local    string `json:"local,omitempty"`
	Redirect bool   `json:"redirect,omitempty"`
}

func Config(options model.Application, request *http.Request) ClientConfig {
	origin := options.LocalNetworkURL
	// A private entry address is useful without setup. A public entry address
	// never proves LAN access, even when the server itself runs on the LAN.
	if origin == "" && request != nil && fn.HostLooksLocalNetwork(request.Host) {
		info := fn.ParseRequestURLTo(request)
		origin, _ = data.NormalizeLocalNetworkURL(info.Origin)
	}
	endpoint := ""
	if origin != "" {
		endpoint = origin + IdentityPath
	}
	return ClientConfig{URL: endpoint, ID: options.LocalNetworkID, Locale: options.Locale}
}

func Script(config ClientConfig) template.JS {
	encoded, _ := json.Marshal(config)
	return template.JS("(function(config){\n" + clientScript + "\n})(" + string(encoded) + ");")
}

func Style() template.HTML {
	return template.HTML("<style>" + controlsCSS + "</style>")
}

func RegisterRouting(e *echo.Echo) {
	handler := func(c *echo.Context) error {
		h := c.Response().Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		// This endpoint exposes only a public random instance identifier. It
		// neither accepts credentials nor returns settings or bookmark data.
		h.Set("Access-Control-Allow-Origin", "*")
		if c.Request().Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			h.Set("Access-Control-Allow-Private-Network", "true")
			return c.NoContent(http.StatusNoContent)
		}
		options, err := data.GetAllSettingsOptions()
		if err != nil || options.LocalNetworkID == "" {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "identity unavailable"})
		}
		return c.JSON(http.StatusOK, map[string]string{"service": "superflare", "id": options.LocalNetworkID})
	}
	e.GET(IdentityPath, handler)
	e.OPTIONS(IdentityPath, handler)
}

func IsHTTPURL(raw string) bool {
	return validation.IsValidHTTPURL(raw)
}
