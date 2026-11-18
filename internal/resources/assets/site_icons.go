package assets

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/junfuchang/superflare/internal/fn"
)

const (
	siteIconStreamMaxSources = 128
	siteIconStreamTimeout    = fn.SiteFaviconFetchTimeout + 5*time.Second
)

type siteIconStreamResult struct {
	Source      string `json:"source"`
	ContentType string `json:"contentType,omitempty"`
	Data        []byte `json:"data,omitempty"`
}

func serveSiteFaviconStream(c *echo.Context) error {
	var input struct {
		Sources []string `json:"sources"`
	}
	body := http.MaxBytesReader(c.Response(), c.Request().Body, 64*1024)
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&input); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid site favicon sources")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid site favicon sources")
	}
	if len(input.Sources) == 0 || len(input.Sources) > siteIconStreamMaxSources {
		return echo.NewHTTPError(http.StatusBadRequest, "too many or missing site favicon sources")
	}
	sources := make(map[string]struct{}, len(input.Sources))
	for _, source := range input.Sources {
		source = strings.TrimSpace(source)
		u, err := url.Parse(source)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid site favicon source")
		}
		sources[source] = struct{}{}
	}
	allowed, err := allowedSiteIconSources(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "icon configuration unavailable")
	}
	for source := range sources {
		if _, ok := allowed[source]; !ok {
			return echo.NewHTTPError(http.StatusForbidden, "site favicon source is not configured or visible")
		}
	}

	// The stream needs time to emit the last result after a favicon times out.
	controller := http.NewResponseController(c.Response())
	if err := controller.SetWriteDeadline(time.Now().Add(siteIconStreamTimeout + time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), siteIconStreamTimeout)
	defer cancel()
	results := make(chan siteIconStreamResult)
	for source := range sources {
		go func() {
			result := siteIconStreamResult{Source: source}
			data, contentType, err := fn.FetchPublicSiteFaviconContext(ctx, source)
			if err == nil {
				result.Data, result.ContentType = data, contentType
			}
			select {
			case results <- result:
			case <-ctx.Done():
			}
		}()
	}

	response := c.Response()
	response.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Accel-Buffering", "no")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(response)
	for range sources {
		select {
		case result := <-results:
			if err := encoder.Encode(result); err != nil {
				return err
			}
			if err := controller.Flush(); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
