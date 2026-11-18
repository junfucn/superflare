package server

import "github.com/labstack/echo/v5"

// Dynamic pages can contain private navigation or account data. Static assets
// and public discovery endpoints explicitly replace this default cache policy.
func privateResponseCache(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "private, no-store")
		return next(c)
	}
}
