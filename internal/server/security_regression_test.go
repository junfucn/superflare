package server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/auth"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func securityRequest(h http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func securityLogin(t *testing.T, h http.Handler, user, pass string) *http.Cookie {
	t.Helper()
	form := url.Values{"username": {user}, "password": {pass}}
	rec := securityRequest(h, http.MethodPost, define.MiscPages.Login.Path, form.Encode(), nil)
	require.Equal(t, http.StatusFound, rec.Code)
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge > 0 {
			return cookie
		}
	}
	t.Fatal("missing login cookie")
	return nil
}

func securityChangePassword(t *testing.T, h http.Handler, cookie *http.Cookie, user, pass string) {
	t.Helper()
	form := url.Values{"login-user": {user}, "login-pass": {pass}, "login-pass-confirm": {pass}}
	rec := securityRequest(h, http.MethodPost, define.SettingPages.Others.Path, form.Encode(), cookie)
	require.Equal(t, http.StatusSeeOther, rec.Code)
}

func TestCredentialChangesRejectReplayedCookies(t *testing.T) {
	for _, user := range []string{"admin", "renamed-admin"} {
		t.Run(user, func(t *testing.T) {
			withRouterTestWorkingDir(t)
			flags := newTestFlags(false, "DEFAULT", true)
			h, err := NewRouter(&flags)
			require.NoError(t, err)
			oldCookie := securityLogin(t, h, "admin", "admin")
			securityChangePassword(t, h, oldCookie, user, "changed-password")
			rec := securityRequest(h, http.MethodGet, "/editor/backup", "", oldCookie)
			require.Equal(t, http.StatusFound, rec.Code)
			newCookie := securityLogin(t, h, user, "changed-password")
			rec = securityRequest(h, http.MethodGet, "/editor/backup", "", newCookie)
			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestLegacySessionRequiresNewLogin(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	flags.CookieSecret = "stable-regression-secret"
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	store := sessions.NewCookieStore([]byte(flags.CookieSecret))
	sess, err := store.New(httptest.NewRequest(http.MethodGet, "/", nil), auth.RequestHandleSessionName(flags.CookieName, flags.Port))
	require.NoError(t, err)
	sess.Values[auth.SESSION_KEY_USER_NAME] = "admin"
	rec := httptest.NewRecorder()
	require.NoError(t, store.Save(httptest.NewRequest(http.MethodGet, "/", nil), rec, sess))
	require.Equal(t, http.StatusFound, securityRequest(h, http.MethodGet, "/editor/backup", "", rec.Result().Cookies()[0]).Code)
}

func TestRestoreUsesEffectiveCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, file, payload, expected string
		zipped                        bool
	}{
		{"bookmarks", "bookmarks.yml", "categories: []\nlinks: []\n", "changed-password", false},
		{"config", "config.yml", "Title: Test\nLocale: en\nTheme: blackboard\nLoginUser: admin\nLoginPass: restored-password\n", "restored-password", false},
		{"zip-config", "config.yml", "Title: Test\nLocale: en\nTheme: blackboard\nLoginUser: admin\nLoginPass: restored-password\n", "restored-password", true},
		{"yaml-looking-password", "config.yml", "Theme: blackboard\nLoginUser: admin\nLoginPass: on\n", "on", false},
		{"zip-numeric-password", "config.yml", "Theme: blackboard\nLoginUser: admin\nLoginPass: 012345\n", "012345", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withRouterTestWorkingDir(t)
			flags := newTestFlags(false, "DEFAULT", true)
			h, err := NewRouter(&flags)
			require.NoError(t, err)
			cookie := securityLogin(t, h, "admin", "admin")
			securityChangePassword(t, h, cookie, "admin", "changed-password")
			cookie = securityLogin(t, h, "admin", "changed-password")
			payload, filename := []byte(tc.payload), tc.file
			if tc.zipped {
				var archive bytes.Buffer
				zw := zip.NewWriter(&archive)
				entry, err := zw.Create(tc.file)
				require.NoError(t, err)
				_, err = entry.Write(payload)
				require.NoError(t, err)
				require.NoError(t, zw.Close())
				payload, filename = archive.Bytes(), "backup.zip"
			}
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("backup", filename)
			require.NoError(t, err)
			_, err = part.Write(payload)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			req := httptest.NewRequest(http.MethodPost, "/editor/restore", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, http.StatusFound, rec.Code)
			securityLogin(t, h, "admin", tc.expected)
			form := url.Values{"username": {"admin"}, "password": {"admin"}}
			require.Equal(t, http.StatusBadRequest, securityRequest(h, http.MethodPost, define.MiscPages.Login.Path, form.Encode(), nil).Code)
			want := http.StatusOK
			if tc.expected != "changed-password" {
				want = http.StatusFound
			}
			require.Equal(t, want, securityRequest(h, http.MethodGet, "/editor/backup", "", cookie).Code)
		})
	}
}

func TestIconSourcesRespectConfigurationAndVisibility(t *testing.T) {
	withRouterTestWorkingDir(t)
	t.Setenv("NO_PROXY", "*")
	var hits atomic.Int32
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><rect width="1" height="1"/></svg>`
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = io.WriteString(w, svg)
	}))
	defer target.Close()
	flags := newTestFlags(false, "PRIVATE", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	for _, path := range []string{"/private-action?value=1", "/favicon.ico"} {
		rec := securityRequest(h, http.MethodGet, "/assets/site-icons?src="+url.QueryEscape(target.URL+path), "", nil)
		require.Equal(t, http.StatusForbidden, rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/assets/site-icons/stream", strings.NewReader(`{"sources":["`+target.URL+`/favicon.ico"]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Zero(t, hits.Load())
	require.NoError(t, data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{{Name: "Private", URL: target.URL, Private: true}}}))
	iconPath := "/assets/site-icons?src=" + url.QueryEscape(target.URL+"/favicon.ico")
	require.Equal(t, http.StatusForbidden, securityRequest(h, http.MethodGet, iconPath, "", nil).Code)
	cookie := securityLogin(t, h, "admin", "admin")
	rec = securityRequest(h, http.MethodGet, iconPath, "", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, svg, rec.Body.String())
	require.Equal(t, int32(1), hits.Load())
	require.Equal(t, http.StatusForbidden, securityRequest(h, http.MethodGet, iconPath, "", nil).Code)
}

func TestBackgroundIsConfiguredAndSandboxed(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "PRIVATE", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	const source = "https://test.invalid/payload.svg"
	const svg = `<svg xmlns="http://www.w3.org/2000/svg"><script>document.documentElement.dataset.test=1</script></svg>`
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
	dir := filepath.Join("var", "cache", "backgrounds")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, key+"-full.bin"), []byte(svg), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, key+"-full.type"), []byte("image/svg+xml"), 0644))
	path := "/assets/background-image?src=" + url.QueryEscape(source)
	require.Equal(t, http.StatusForbidden, securityRequest(h, http.MethodGet, path, "", nil).Code)
	require.NoError(t, os.WriteFile("config.yml", []byte("Title: Test\nTheme: blackboard\nBackgroundImage: "+source+"\n"), 0644))
	data.InvalidateConfigCache("config")
	rec := securityRequest(h, http.MethodGet, path, "", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, svg, rec.Body.String())
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "script-src 'none'")
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "sandbox")
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

func TestEditorEscapesScriptClosingTags(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	cookie := securityLogin(t, h, "admin", "admin")
	const payload = `</script><script id=injected>document.documentElement.dataset.test=1</script>`
	require.NoError(t, data.SaveNormalBookmarks(model.Bookmarks{Items: []model.Bookmark{{Name: "Test", URL: "https://test.invalid/", Desc: payload}}}))
	rec := securityRequest(h, http.MethodGet, "/editor", "", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	tokens := html.NewTokenizer(strings.NewReader(rec.Body.String()))
	for tokens.Next() != html.ErrorToken {
		for _, attr := range tokens.Token().Attr {
			require.NotEqual(t, "injected", attr.Val)
		}
	}
	require.Contains(t, rec.Body.String(), `\u003c/script\u003e`)
}

func TestUnauthenticatedEditorSaveReturns401JSON(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/editor", strings.NewReader("categories=&bookmarks="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json,text/html;q=0.8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.Empty(t, rec.Header().Get("Location"))
}
