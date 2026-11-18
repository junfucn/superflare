package home

import (
	"errors"
	"mime"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"
)

const (
	searchFormMaxBytes  = 8 << 10
	searchQueryMaxRunes = 50
)

func readSearchForm(c *echo.Context) (url.Values, error) {
	req := c.Request()
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return nil, echo.NewHTTPError(http.StatusUnsupportedMediaType, "Search requires a URL-encoded form")
	}
	if req.ContentLength > searchFormMaxBytes {
		return nil, echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Search form is too large")
	}
	req.Body = http.MaxBytesReader(c.Response(), req.Body, searchFormMaxBytes)
	if err := req.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, echo.NewHTTPError(http.StatusRequestEntityTooLarge, "Search form is too large")
		}
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid search form")
	}
	return req.PostForm, nil
}
