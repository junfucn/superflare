package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/internal/localnetwork"
	"github.com/stretchr/testify/require"
)

func TestLegacyRestoreKeepsActiveAccountAndIdentityImmediately(t *testing.T) {
	for _, tc := range []struct {
		name, identity string
		zipped         bool
	}{
		{name: "yaml-legacy"},
		{name: "zip-legacy", zipped: true},
		{name: "yaml-explicit-identity", identity: "restored-instance-identity"},
		{name: "zip-explicit-identity", identity: "restored-instance-identity", zipped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withRouterTestWorkingDir(t)
			flags := newTestFlags(false, "DEFAULT", true)
			h, err := NewRouter(&flags)
			require.NoError(t, err)
			cookie := securityLogin(t, h, "admin", "admin")
			securityChangePassword(t, h, cookie, "owner", "owner-strong-password")
			cookie = securityLogin(t, h, "owner", "owner-strong-password")
			before, err := data.GetAllSettingsOptions()
			require.NoError(t, err)
			require.NotEmpty(t, before.LocalNetworkID)
			payload := []byte("Title: Legacy\nLocale: en\nTheme: blackboard\n")
			if tc.identity != "" {
				payload = append(payload, []byte("LocalNetworkID: "+tc.identity+"\n")...)
			}
			filename := "config.yml"
			if tc.zipped {
				var archive bytes.Buffer
				zw := zip.NewWriter(&archive)
				entry, err := zw.Create(filename)
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
			require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
			securityLogin(t, h, "owner", "owner-strong-password")
			form := url.Values{"username": {"admin"}, "password": {"admin"}}
			require.Equal(t, http.StatusBadRequest, securityRequest(h, http.MethodPost, define.MiscPages.Login.Path, form.Encode(), nil).Code)
			require.Equal(t, http.StatusOK, securityRequest(h, http.MethodGet, "/editor/backup", "", cookie).Code)
			rec = securityRequest(h, http.MethodGet, localnetwork.IdentityPath, "", nil)
			require.Equal(t, http.StatusOK, rec.Code)
			var identity struct{ ID string }
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &identity))
			want := before.LocalNetworkID
			if tc.identity != "" {
				want = tc.identity
			}
			require.Equal(t, want, identity.ID)
		})
	}
}
