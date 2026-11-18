package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/model"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type countedLoginBody struct {
	io.Reader
	read int
}

func (b *countedLoginBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}

func TestLoginRejectsUnsafeBodiesBeforeAuthentication(t *testing.T) {
	original := SnapshotAuthRuntimeConfig()
	t.Cleanup(func() { StoreAuthRuntimeConfig(original) })
	e := echo.New()
	RequestHandleWithFlags(e, model.Flags{Port: 3636, CookieName: "bounded-login", CookieSecret: "test-secret", User: "admin", Pass: "admin"})
	for _, tc := range []struct {
		name, contentType, body, query string
		length                         int64
		status, maxRead                int
	}{
		{"multipart", "multipart/form-data; boundary=probe", "--probe\r\n", "", -1, http.StatusUnsupportedMediaType, 0},
		{"json", "application/json", `{}`, "", -1, http.StatusUnsupportedMediaType, 0},
		{"known-large-form", echo.MIMEApplicationForm, "username=admin&password=admin", "", loginFormMaxBytes + 1, http.StatusRequestEntityTooLarge, 0},
		{"streamed-large-form", echo.MIMEApplicationForm, "username=admin&password=admin&extra=" + strings.Repeat("x", loginFormMaxBytes*2), "", -1, http.StatusRequestEntityTooLarge, loginFormMaxBytes + 1},
		{"invalid-encoding", echo.MIMEApplicationForm, "username=admin&password=admin&extra=%zz", "", -1, http.StatusBadRequest, 128},
		{"query-credentials", echo.MIMEApplicationForm, "", "?username=admin&password=admin", 0, http.StatusBadRequest, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &countedLoginBody{Reader: strings.NewReader(tc.body)}
			req := httptest.NewRequest(http.MethodPost, "/login"+tc.query, body)
			req.ContentLength = tc.length
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
			require.LessOrEqual(t, body.read, tc.maxRead)
			require.Nil(t, req.MultipartForm)
			require.Empty(t, rec.Header().Get("Set-Cookie"))
		})
	}
}

func TestSameOriginGuardUsesBrowserMetadataAndOriginFallback(t *testing.T) {
	for _, tc := range []struct {
		name, method, origin, fetchSite string
		allowed                         bool
	}{
		{"modern-same-origin", http.MethodPost, "https://nav.example.test", "same-origin", true},
		{"legacy-same-origin", http.MethodPost, "https://nav.example.test", "", true},
		{"non-browser-client", http.MethodPost, "", "", true},
		{"sibling-subdomain", http.MethodPost, "https://other.example.test", "same-site", false},
		{"different-port", http.MethodPost, "https://nav.example.test:8443", "", false},
		{"legacy-cross-origin", http.MethodPost, "https://other.example.test", "", false},
		{"opaque-origin", http.MethodPost, "null", "", false},
		{"cross-site-without-origin", http.MethodPost, "", "cross-site", false},
		{"missing-origin-same-site", http.MethodPost, "", "same-site", false},
		{"cross-origin-safe-navigation", http.MethodGet, "https://other.example.test", "cross-site", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(tc.method, "http://nav.example.test/settings/theme", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			req.Header.Set("X-Forwarded-Proto", "https")
			req.Header.Set("X-Forwarded-Host", "other.example.test")
			called := false
			err := requireSameOrigin(func(c *echo.Context) error { called = true; return nil })(e.NewContext(req, httptest.NewRecorder()))
			require.Equal(t, tc.allowed, called)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Equal(t, http.StatusForbidden, echo.StatusCode(err))
			}
		})
	}
}
