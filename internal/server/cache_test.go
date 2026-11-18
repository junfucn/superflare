package server

import (
	"net/http"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/stretchr/testify/require"
)

func TestPrivatePagesCannotBeCached(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	h, err := NewRouter(&flags)
	require.NoError(t, err)
	require.NoError(t, data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{{Name: "private-cache-sentinel", URL: "https://private.invalid", Private: true}}}))
	cookie := securityLogin(t, h, "admin", "admin")
	for _, path := range []string{"/", "/editor", "/editor/backup", define.SettingPages.Others.Path} {
		rec := securityRequest(h, http.MethodGet, path, "", cookie)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
		if path == "/" {
			require.Contains(t, rec.Body.String(), "private-cache-sentinel")
		}
		rec = securityRequest(h, http.MethodGet, path, "", nil)
		require.Contains(t, rec.Header().Get("Cache-Control"), "no-store")
		require.NotContains(t, rec.Body.String(), "private-cache-sentinel")
	}
	for _, path := range []string{"/favicon.ico", "/robots.txt"} {
		rec := securityRequest(h, http.MethodGet, path, "", nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Header().Get("Cache-Control"), "public")
	}
}
