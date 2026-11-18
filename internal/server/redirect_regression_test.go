package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestRedirectHelpersRespectPrivateBookmarks(t *testing.T) {
	for _, disableLogin := range []bool{false, true} {
		t.Run(fmt.Sprintf("disableLogin=%t", disableLogin), func(t *testing.T) {
			withRouterTestWorkingDir(t)
			flags := newTestFlags(disableLogin, "PRIVATE", true)
			handler, err := NewRouter(&flags)
			require.NoError(t, err)
			var signedIn *http.Cookie
			if !disableLogin {
				signedIn = securityLogin(t, handler, "admin", "admin")
			}
			for _, collection := range []struct {
				name string
				save func(model.Bookmarks) error
			}{
				{"apps", data.SaveFavoriteBookmarks},
				{"bookmarks", data.SaveNormalBookmarks},
			} {
				for _, private := range []bool{false, true} {
					target := fmt.Sprintf("https://service.example.test/%s/%t", collection.name, private)
					local := fmt.Sprintf("http://192.168.1.2/%s/%t", collection.name, private)
					require.NoError(t, collection.save(model.Bookmarks{Items: []model.Bookmark{{Name: "Test", URL: target, LocalURL: local, Private: private}}}))
					for _, signed := range []bool{false, true} {
						var cookie *http.Cookie
						if signed {
							cookie = signedIn
							if cookie == nil {
								continue
							}
						}
						for _, route := range []struct {
							path string
							code int
						}{
							{define.MiscPages.RedirHelper.Path, http.StatusFound},
							{define.MiscPages.RedirLocal.Path, http.StatusOK},
						} {
							path := route.path + "?go=" + data.Base64EncodeUrl(target) + "&local=" + data.Base64EncodeUrl(local)
							response := securityRequest(handler, http.MethodGet, path, "", cookie)
							want := route.code
							if private && !disableLogin && !signed {
								want = http.StatusBadRequest
								require.NotContains(t, response.Body.String(), target)
								require.NotContains(t, response.Body.String(), local)
								require.Empty(t, response.Header().Get("Location"))
							} else if route.code == http.StatusFound {
								require.Equal(t, target, response.Header().Get("Location"))
							} else {
								require.Contains(t, response.Body.String(), target)
								require.Contains(t, response.Body.String(), local)
							}
							require.Equal(t, want, response.Code, "%s private=%t signed=%t: %s", path, private, signed, response.Body.String())
							require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
						}
					}
				}
			}
		})
	}
}

func TestRedirectHelpersPreserveBase64Plus(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)
	const target = "https://example.com/~"
	const local = "http://192.168.1.2:5000/~"
	require.NoError(t, data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{{Name: "Test", URL: target, LocalURL: local}}}))
	for _, path := range []string{
		define.MiscPages.RedirHelper.Path + "?go=" + data.Base64EncodeUrl(target),
		define.MiscPages.RedirLocal.Path + "?go=" + data.Base64EncodeUrl(target) + "&local=" + data.Base64EncodeUrl(local),
	} {
		request := httptest.NewRequest(http.MethodGet, "http://nav.example.test"+path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if strings.HasPrefix(path, define.MiscPages.RedirLocal.Path+"?") {
			require.Equal(t, http.StatusOK, response.Code)
			require.Contains(t, response.Body.String(), target)
			require.Contains(t, response.Body.String(), local)
			require.Contains(t, response.Body.String(), "value.id === config.id")
			continue
		}
		require.Equal(t, http.StatusFound, response.Code, response.Body.String())
		require.Equal(t, target, response.Header().Get("Location"))
	}
}

func TestDynamicRedirectUsesConfiguredProxyScheme(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	flags.TrustedProxies = "10.0.0.2/32"
	handler, err := NewRouter(&flags)
	require.NoError(t, err)
	require.NoError(t, data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{{Name: "Test", URL: "{origin}/app"}}}))
	for _, tc := range []struct{ peer, target string }{
		{"10.0.0.2:1234", "https://nav.example.test/app"},
		{"93.184.216.34:1234", "http://nav.example.test/app"},
	} {
		request := httptest.NewRequest(http.MethodGet, "http://nav.example.test"+define.MiscPages.RedirHelper.Path+"?go="+data.Base64EncodeUrl(tc.target), nil)
		request.RemoteAddr = tc.peer
		request.Header.Set("X-Forwarded-Proto", "https")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusFound, response.Code, response.Body.String())
		require.Equal(t, tc.target, response.Header().Get("Location"))
	}
	flags.TrustedProxies = "not-an-ip"
	_, err = NewRouter(&flags)
	require.ErrorContains(t, err, "invalid trusted proxy")
}

func TestEncryptedRedirectRejectsExecutableBookmarkTargets(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)
	for _, target := range []string{"javascript:alert(1)", "vbscript:msgbox(1)", "data:text/html,unsafe"} {
		require.NoError(t, data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{{Name: "Unsafe", URL: target}}}))
		response := securityRequest(handler, http.MethodGet, define.MiscPages.RedirHelper.Path+"?go="+data.Base64EncodeUrl(target), "", nil)
		require.Equal(t, http.StatusBadRequest, response.Code, target)
		require.Empty(t, response.Header().Get("Location"), target)
	}
}

func TestPageDynamicRedirectsFromRenderedLinks(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	flags.TrustedProxies = "10.0.0.2/32"
	handler, err := NewRouter(&flags)
	require.NoError(t, err)
	options, err := data.GetAllSettingsOptions()
	require.NoError(t, err)
	options.ShowApps, options.ShowBookmarks, options.ShowFavorites = true, true, true
	options.EnableEncryptedLink = true
	options.LocalNetworkDirectFallback = false
	require.NoError(t, data.UpdateAppearance(options))
	items := model.Bookmarks{Items: []model.Bookmark{
		{Name: "Path", URL: "https://service.example.test/open?from={pathname}"},
		{Name: "Page", URL: "https://service.example.test/open?from={href}"},
		{Name: "Local", URL: "https://service.example.test/local?from={href}", LocalURL: "http://192.168.1.2/open?from={pathname}", Favorite: true},
	}}
	require.NoError(t, data.SaveFavoriteBookmarks(items))
	require.NoError(t, data.SaveNormalBookmarks(items))
	for _, page := range []string{"/", "/applications?view=a%2Bb&return=%2Fbookmarks", "/bookmarks?view=a+b"} {
		for _, peer := range []string{"10.0.0.2:1234", "93.184.216.34:1234"} {
			t.Run(page+"/"+peer, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, "http://nav.example.test"+page, nil)
				request.RemoteAddr = peer
				request.Header.Set("X-Forwarded-Proto", "https")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				origin := "http://nav.example.test"
				if peer == "10.0.0.2:1234" {
					origin = "https://nav.example.test"
				}
				expected := map[string]bool{
					"https://service.example.test/open?from=" + request.URL.Path: false,
					"https://service.example.test/open?from=" + origin + page:    false,
					"https://service.example.test/local?from=" + origin + page:   false,
				}
				networkAttributes := map[string]bool{"data-network-source": false, "data-network-redirect": false}
				tokens := html.NewTokenizer(strings.NewReader(response.Body.String()))
				for {
					if tokens.Next() == html.ErrorToken {
						require.ErrorIs(t, tokens.Err(), io.EOF)
						break
					}
					token := tokens.Token()
					if token.Data != "a" || token.Type != html.StartTagToken {
						continue
					}
					for _, attr := range token.Attr {
						if attr.Key != "href" && attr.Key != "data-network-source" && attr.Key != "data-network-redirect" {
							continue
						}
						link, err := url.Parse(attr.Val)
						require.NoError(t, err)
						if link.Path != define.MiscPages.RedirHelper.Path && link.Path != define.MiscPages.RedirLocal.Path {
							continue
						}
						require.Equal(t, page, link.Query().Get("from"))
						target, err := data.Base64DecodeUrl(link.Query().Get("go"))
						require.NoError(t, err)
						require.Contains(t, expected, string(target))
						expected[string(target)] = true
						if _, ok := networkAttributes[attr.Key]; ok {
							networkAttributes[attr.Key] = true
						}
						follow := httptest.NewRequest(http.MethodGet, "http://nav.example.test"+attr.Val, nil)
						follow.RemoteAddr = peer
						follow.Header.Set("X-Forwarded-Proto", "https")
						result := httptest.NewRecorder()
						handler.ServeHTTP(result, follow)
						if link.Path == define.MiscPages.RedirHelper.Path {
							require.Equal(t, http.StatusFound, result.Code, attr.Val+": "+result.Body.String())
							require.Equal(t, string(target), result.Header().Get("Location"))
						} else {
							require.Equal(t, http.StatusOK, result.Code, attr.Val+": "+result.Body.String())
							local, err := data.Base64DecodeUrl(link.Query().Get("local"))
							require.NoError(t, err)
							require.Equal(t, "http://192.168.1.2/open?from="+request.URL.Path, string(local))
							require.Contains(t, result.Body.String(), string(local))
						}
					}
				}
				for target, found := range expected {
					require.True(t, found, "missing rendered redirect for %s", target)
				}
				for attr, found := range networkAttributes {
					require.True(t, found, "missing %s", attr)
				}
			})
		}
	}
}

func TestPageDynamicRedirectsRejectTampering(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", true)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)
	require.NoError(t, data.SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{
		{Name: "Path", URL: "https://service.example.test/open?from={pathname}", LocalURL: "http://192.168.1.2/open?from={pathname}"},
		{Name: "Page", URL: "{href}", LocalURL: "http://192.168.1.3/open?from={pathname}"},
	}}))
	const source = "https://service.example.test/open?from=/applications"
	const local = "http://192.168.1.2/open?from=/applications"
	for _, tc := range []struct{ name, page, source, local string }{
		{"changed page", "/bookmarks", source, local},
		{"external origin", "https://evil.example.test/applications", "https://evil.example.test/applications", local},
		{"network path", "//evil.example.test/applications", source, local},
		{"unsupported page", "/settings", source, local},
		{"fragment", "/applications#section", source, local},
		{"empty page", "", source, local},
		{"arbitrary source", "/applications", "https://evil.example.test/", local},
		{"mixed pair", "/applications", source, "http://192.168.1.3/open?from=/applications"},
		{"other local page", "/applications", source, "http://192.168.1.2/open?from=/bookmarks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{define.MiscPages.RedirHelper.Path, define.MiscPages.RedirLocal.Path} {
				if path == define.MiscPages.RedirHelper.Path && (tc.name == "mixed pair" || tc.name == "other local page") {
					continue
				}
				address := path + "?go=" + data.Base64EncodeUrl(tc.source) + "&from=" + url.QueryEscape(tc.page)
				if path == define.MiscPages.RedirLocal.Path {
					address += "&local=" + data.Base64EncodeUrl(tc.local)
				}
				request := httptest.NewRequest(http.MethodGet, "http://nav.example.test"+address, nil)
				request.RemoteAddr = "93.184.216.34:1234"
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				require.Equal(t, http.StatusBadRequest, response.Code, address+": "+response.Body.String())
			}
		})
	}
}
