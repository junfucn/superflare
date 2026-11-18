package server

import (
	"net/http"
	"os"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/stretchr/testify/require"
)

func TestPortCollectionSettingRequiresLoginAndCanBeToggled(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	path := define.SettingPages.Ports.Path + "/collection"
	before, err := os.ReadFile("config.yml")
	require.NoError(t, err)

	rec := securityRequest(h, http.MethodPost, path, "", nil)
	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, define.SettingPages.Others.Path, rec.Header().Get("Location"))
	after, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)

	cookie := securityLogin(t, h, "admin", "admin")
	for _, tc := range []struct {
		form    string
		enabled bool
	}{
		{form: "", enabled: false},
		{form: "enabled=true", enabled: true},
	} {
		rec = securityRequest(h, http.MethodPost, path, tc.form, cookie)
		require.Equal(t, http.StatusSeeOther, rec.Code)
		target := rec.Header().Get("Location")
		require.Equal(t, define.SettingPages.Ports.Path+"?saved=1", target)
		options, err := data.GetAllSettingsOptions()
		require.NoError(t, err)
		require.Equal(t, !tc.enabled, options.DisablePortCollection)
		rec = securityRequest(h, http.MethodGet, target, "", cookie)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), `class="settings-save-notice"`)
		require.Contains(t, rec.Body.String(), `id="form-port-collection"`)
		require.Contains(t, rec.Body.String(), `action="`+path+`"`)
		require.Contains(t, rec.Body.String(), `name="enabled"`)
		if tc.enabled {
			require.Contains(t, rec.Body.String(), `name="enabled" value="true" checked`)
		} else {
			require.NotContains(t, rec.Body.String(), `name="enabled" value="true" checked`)
		}
	}

	before, err = os.ReadFile("config.yml")
	require.NoError(t, err)
	rec = securityRequest(h, http.MethodPost, path, "enabled=invalid", cookie)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "status-panel")
	after, err = os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)
}
