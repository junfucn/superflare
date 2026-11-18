package server

import (
	"bytes"
	"mime/multipart"
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

func TestOversizedCredentialsDoNotChangeFilesOrSession(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(".env", []byte("FLARE_USER=admin\nFLARE_PASS=admin\n"), 0600))
	cookie := securityLogin(t, h, "admin", "admin")
	configBefore, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	envBefore, err := os.ReadFile(".env")
	require.NoError(t, err)
	for _, tc := range []struct{ name, user, pass string }{
		{"username", strings.Repeat("u", data.LoginUserMaxBytes+1), "valid-pass"},
		{"password", "admin", strings.Repeat("p", data.LoginPassMaxBytes+1)},
		{"unicode-username", strings.Repeat("\u7528", data.LoginUserMaxBytes/3+1), "valid-pass"},
		{"unicode-password", "admin", strings.Repeat("\u5bc6", data.LoginPassMaxBytes/3+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"login-user": {tc.user}, "login-pass": {tc.pass}, "login-pass-confirm": {tc.pass}}
			rec := securityRequest(h, http.MethodPost, define.SettingPages.Others.Path, form.Encode(), cookie)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, http.StatusOK, securityRequest(h, http.MethodGet, "/editor/backup", "", cookie).Code)
			configAfter, err := os.ReadFile("config.yml")
			require.NoError(t, err)
			envAfter, err := os.ReadFile(".env")
			require.NoError(t, err)
			require.Equal(t, configBefore, configAfter)
			require.Equal(t, envBefore, envAfter)
		})
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("backup", "config.yml")
	require.NoError(t, err)
	_, err = part.Write([]byte("Title: Oversized\nLocale: en\nTheme: blackboard\nLoginUser: admin\nLoginPass: " + strings.Repeat("p", data.LoginPassMaxBytes+1) + "\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/editor/restore", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	configAfter, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, configBefore, configAfter)
	envAfter, err := os.ReadFile(".env")
	require.NoError(t, err)
	require.Equal(t, envBefore, envAfter)
	require.Equal(t, http.StatusOK, securityRequest(h, http.MethodGet, "/editor/backup", "", cookie).Code)
}

func TestMaximumCredentialsCanLoginWithEncodedFormAndCookie(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	cookie := securityLogin(t, h, "admin", "admin")
	user := strings.Repeat("%", data.LoginUserMaxBytes)
	pass := strings.Repeat("&", data.LoginPassMaxBytes)
	securityChangePassword(t, h, cookie, user, pass)
	cookie = securityLogin(t, h, user, pass)
	require.Less(t, len(cookie.String()), 4096)
	require.Equal(t, http.StatusOK, securityRequest(h, http.MethodGet, "/editor/backup", "", cookie).Code)
}
