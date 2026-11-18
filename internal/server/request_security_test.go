package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/stretchr/testify/require"
)

func TestCrossOriginWritesAreRejectedAcrossManagementRoutes(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "authenticated", true: "login-disabled"}[disabled], func(t *testing.T) {
			withRouterTestWorkingDir(t)
			flags := newTestFlags(disabled, "DEFAULT", true)
			h, err := NewRouter(&flags)
			require.NoError(t, err)
			var cookie *http.Cookie
			if !disabled {
				cookie = securityLogin(t, h, "admin", "admin")
			}
			paths := []string{
				"/editor", "/editor/restore", "/editor/check-links",
				define.SettingPages.Theme.Path, define.SettingPages.Search.Path,
				define.SettingPages.Appearance.Path, define.SettingPages.Ports.Path,
				define.SettingPages.Ports.Path + "/collection",
				define.SettingPages.Others.Path, define.SettingPages.SEO.Path, define.SettingPages.Scripts.Path,
			}
			if !disabled {
				paths = append(paths, "/login", "/logout")
			}
			before, err := data.GetAllSettingsOptions()
			require.NoError(t, err)
			form := url.Values{"login-user": {"admin"}, "login-pass": {"unwanted-password"}, "login-pass-confirm": {"unwanted-password"}}
			for _, path := range paths {
				req := httptest.NewRequest(http.MethodPost, "https://nav.example.test"+path, strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Origin", "https://untrusted.example.test")
				req.Header.Set("Sec-Fetch-Site", "same-site")
				if cookie != nil {
					req.AddCookie(cookie)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				require.Equal(t, http.StatusForbidden, rec.Code, path)
				require.Nil(t, req.Form, path)
				require.Empty(t, rec.Header().Get("Set-Cookie"), path)
			}
			after, err := data.GetAllSettingsOptions()
			require.NoError(t, err)
			require.Equal(t, before, after)
			if cookie != nil {
				require.Equal(t, http.StatusOK, securityRequest(h, http.MethodGet, "/editor/backup", "", cookie).Code)
			}
		})
	}
}

func TestSessionCookieSchemeRespectsTrustedProxies(t *testing.T) {
	for _, tc := range []struct {
		name, trusted, peer, scheme, header, value string
		secure                                     bool
	}{
		{"untrusted-xfp", "", "203.0.113.1:5000", "http", "X-Forwarded-Proto", "https", false},
		{"trust-disabled", "none", "127.0.0.1:5000", "http", "X-Forwarded-Proto", "https", false},
		{"trusted-forwarded", "", "127.0.0.1:5000", "http", "Forwarded", "for=192.0.2.1;proto=https", true},
		{"trusted-cidr", "10.0.0.0/8", "10.1.2.3:5000", "http", "X-Forwarded-Proto", "https", true},
		{"direct-tls", "none", "203.0.113.1:5000", "https", "X-Forwarded-Proto", "http", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withRouterTestWorkingDir(t)
			flags := newTestFlags(false, "DEFAULT", true)
			flags.TrustedProxies = tc.trusted
			h, err := NewRouter(&flags)
			require.NoError(t, err)
			var sessionCookie *http.Cookie
			for _, route := range []string{"/login", "/logout"} {
				req := httptest.NewRequest(http.MethodPost, tc.scheme+"://nav.example.test"+route, strings.NewReader("username=admin&password=admin"))
				req.RemoteAddr = tc.peer
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set(tc.header, tc.value)
				if sessionCookie != nil {
					req.AddCookie(sessionCookie)
				}
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				require.Equal(t, http.StatusFound, rec.Code)
				cookies := rec.Result().Cookies()
				require.NotEmpty(t, cookies)
				for _, cookie := range cookies {
					require.Equal(t, tc.secure, cookie.Secure, route)
					if route == "/logout" {
						require.Negative(t, cookie.MaxAge)
					}
				}
				sessionCookie = cookies[0]
			}
		})
	}
}

func TestSettingsReadDoesNotResetValidCredentials(t *testing.T) {
	withRouterTestWorkingDir(t)
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, data.UpdateLoginConfig("saved-user", "saved-strong-password"))
	env := []byte("FLARE_COOKIE_SECRET=separate-secret\n")
	require.NoError(t, os.WriteFile(".env", env, 0600))
	flags := newTestFlags(false, "DEFAULT", true)
	flags.User, flags.Pass = "saved-user", "saved-strong-password"
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	cookie := securityLogin(t, h, flags.User, flags.Pass)
	before, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	rec := securityRequest(h, http.MethodGet, define.SettingPages.Others.Path, "", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	after, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)
	afterEnv, err := os.ReadFile(".env")
	require.NoError(t, err)
	require.Equal(t, env, afterEnv)
}

func TestSameOriginLoginAndEditorSaveRemainAvailable(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	login := httptest.NewRequest(http.MethodPost, "https://nav.example.test/login", strings.NewReader("username=admin&password=admin"))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	login.Header.Set("Origin", "https://nav.example.test")
	login.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, login)
	require.Equal(t, http.StatusFound, rec.Code)
	require.NotEmpty(t, rec.Result().Cookies())
	cookie := rec.Result().Cookies()[0]
	snapshot, err := data.GetEditorSnapshot()
	require.NoError(t, err)
	page := securityRequest(h, http.MethodGet, "/editor", "", cookie)
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), `id="field-revision" value="`+snapshot.Revision+`"`)
	form := url.Values{"categories": {""}, "bookmarks": {"1,Safe,https://example.com,,,,,,,"}, "revision": {snapshot.Revision}}
	save := httptest.NewRequest(http.MethodPost, "http://nav.example.test/editor", strings.NewReader(form.Encode()))
	save.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	save.Header.Set("Origin", "https://nav.example.test")
	save.Header.Set("X-Forwarded-Proto", "https")
	save.Header.Set("Accept", "application/json")
	save.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, save)
	require.Equal(t, http.StatusOK, rec.Code)
	saved, err := data.GetEditorSnapshot()
	require.NoError(t, err)
	require.JSONEq(t, `{"notice":"save_success","revision":"`+saved.Revision+`"}`, rec.Body.String())
	apps, err := data.LoadFavoriteBookmarks()
	require.NoError(t, err)
	require.Len(t, apps.Items, 1)
	require.Equal(t, "Safe", apps.Items[0].Name)
}
