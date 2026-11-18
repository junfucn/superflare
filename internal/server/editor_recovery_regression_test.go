package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/stretchr/testify/require"
)

func TestRestoreKeepsLocalNetworkIdentityAvailable(t *testing.T) {
	for _, tc := range []struct {
		name, identity string
		zipped         bool
	}{
		{name: "legacy-yaml"},
		{name: "legacy-zip", zipped: true},
		{name: "empty-identity", identity: "LocalNetworkID: \"\"\n"},
		{name: "existing-identity", identity: "LocalNetworkID: backup-instance\n", zipped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withRouterTestWorkingDir(t)
			flags := newTestFlags(false, "DEFAULT", true)
			h, err := NewRouter(&flags)
			require.NoError(t, err)
			cookie := securityLogin(t, h, "admin", "admin")
			before, err := data.GetAllSettingsOptions()
			require.NoError(t, err)
			require.NotEmpty(t, before.LocalNetworkID)
			payload := []byte("Title: Legacy\nLocale: zh\nTheme: blackboard\nLoginUser: admin\nLoginPass: admin\nLocalNetworkURL: https://nav.home.arpa\n" + tc.identity)
			filename := "config.yml"
			if tc.zipped {
				var archive bytes.Buffer
				zw := zip.NewWriter(&archive)
				entry, err := zw.Create(filename)
				require.NoError(t, err)
				_, err = entry.Write(payload)
				require.NoError(t, err)
				require.NoError(t, zw.Close())
				filename, payload = "backup.zip", archive.Bytes()
			}
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("backup", filename)
			require.NoError(t, err)
			_, err = part.Write(payload)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			request := httptest.NewRequest(http.MethodPost, "/editor/restore", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			require.Equal(t, http.StatusFound, response.Code, response.Body.String())
			require.Contains(t, response.Header().Get("Location"), "restore_success")
			identity := securityRequest(h, http.MethodGet, "/api/local-network/identity", "", nil)
			require.Equal(t, http.StatusOK, identity.Code, identity.Body.String())
			var actual struct{ Service, ID string }
			require.NoError(t, json.Unmarshal(identity.Body.Bytes(), &actual))
			want := before.LocalNetworkID
			if tc.name == "existing-identity" {
				want = "backup-instance"
			}
			require.Equal(t, "superflare", actual.Service)
			require.Equal(t, want, actual.ID)
		})
	}
}

func TestEditorRejectsSaveThatWouldBreakBackup(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	cookie := securityLogin(t, h, "admin", "admin")
	before, err := os.ReadFile("apps.yml")
	require.NoError(t, err)
	snapshot, err := data.GetEditorSnapshot()
	require.NoError(t, err)
	form := url.Values{"categories": {""}, "bookmarks": {"1,Large,https://example.invalid/,,,,," + strings.Repeat("x", data.MaxConfigFileSize) + ",false,false"}, "revision": {snapshot.Revision}}
	request := httptest.NewRequest(http.MethodPost, "/editor", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "8 MiB limit")
	after, err := os.ReadFile("apps.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)
	backup := securityRequest(h, http.MethodGet, "/editor/backup", "", cookie)
	require.Equal(t, http.StatusOK, backup.Code, backup.Body.String())
}

func TestSitemapIgnoresForwardedHostWhenProxyTrustIsDisabled(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	flags.TrustedProxies = "none"
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "http://nav.example.test/sitemap.xml", nil)
	request.RemoteAddr = "93.184.216.34:1234"
	request.Header.Set("X-Forwarded-Host", "attacker.example:9443")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "http://nav.example.test/")
	require.NotContains(t, response.Body.String(), "attacker.example")
}
