package auth

import (
	"errors"
	"mime"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"
)

const loginFormMaxBytes = 16 << 10

func requireSameOrigin(next echo.HandlerFunc) echo.HandlerFunc {
	protection := http.NewCrossOriginProtection()
	return func(c *echo.Context) error {
		if err := protection.Check(c.Request()); err != nil {
			c.Response().Header().Set("Cache-Control", "no-store")
			return echo.NewHTTPError(http.StatusForbidden, "Cross-origin requests are not allowed")
		}
		return next(c)
	}
}

func readLoginForm(c *echo.Context) (url.Values, error) {
	req := c.Request()
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return nil, echo.NewHTTPError(http.StatusUnsupportedMediaType, "Login requires a URL-encoded form")
	}
	if req.ContentLength > loginFormMaxBytes {
		return nil, echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Login form is too large")
	}
	req.Body = http.MaxBytesReader(c.Response(), req.Body, loginFormMaxBytes)
	if err := req.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Login form is too large")
		}
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid login form")
	}
	return req.PostForm, nil
}
