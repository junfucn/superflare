package server

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/stretchr/testify/require"
)

const customScriptsFixture = `<!-- Google tag (gtag.js) -->
<script async src="https://www.googletagmanager.com/gtag/js?id=G-TEST"></script>
<script>
  window.dataLayer = window.dataLayer || [];
  function gtag(){dataLayer.push(arguments);}
  gtag('js', new Date());
  gtag('config', 'G-TEST');
</script>
<script nonce="old-nonce">
  var _hmt = _hmt || [];
  (function() {
    var hm = document.createElement("script");
    hm.src = "https://hm.baidu.com/hm.js?test";
    var s = document.getElementsByTagName("script")[0];
    s.parentNode.insertBefore(hm, s);
  })();
</script>`

func TestCustomScriptsSaveRenderAndClear(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(true, "DEFAULT", false)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)
	before, err := data.GetAllSettingsOptions()
	require.NoError(t, err)

	postScripts := func(code string) *httptest.ResponseRecorder {
		form := url.Values{"custom-scripts": {code}}
		req := httptest.NewRequest(http.MethodPost, "/settings/scripts", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return rec
	}

	// Use the real settings renderer to verify pasted HTML stays editable.
	settings := postScripts(customScriptsFixture).Body.String()
	require.Contains(t, settings, `name="custom-scripts"`)
	require.Contains(t, settings, template.HTMLEscapeString(customScriptsFixture))
	require.NotContains(t, settings, `<script nonce="old-nonce">`)
	require.NotContains(t, settings, "<no value>")

	raw, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	persisted, err := data.LoadAppConfigFromRaw(raw)
	require.NoError(t, err)
	require.Equal(t, customScriptsFixture, persisted.CustomScripts)
	require.Equal(t, before.Title, persisted.Title)
	require.Equal(t, before.Theme, persisted.Theme)
	require.Equal(t, before.Footer, persisted.Footer)

	// Reinitialize the router to also cover loading saved configuration.
	handler, err = NewRouter(&flags)
	require.NoError(t, err)
	nonces := map[string]bool{}
	for _, path := range []string{"/", "/applications", "/bookmarks", "/help"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, path)
		head, _, ok := strings.Cut(rec.Body.String(), "</head>")
		require.True(t, ok, path)
		require.Contains(t, head, `src="https://www.googletagmanager.com/gtag/js?id=G-TEST"`)
		require.Contains(t, head, "function gtag(){dataLayer.push(arguments);}")
		require.Contains(t, head, "gtag('config', 'G-TEST');")
		require.Contains(t, head, `hm.src = "https://hm.baidu.com/hm.js?test";`)
		require.NotContains(t, head, "old-nonce")

		csp := rec.Header().Get("Content-Security-Policy")
		const prefix = "script-src 'nonce-"
		require.True(t, strings.HasPrefix(csp, prefix), csp)
		nonce, _, ok := strings.Cut(strings.TrimPrefix(csp, prefix), "'")
		require.True(t, ok)
		require.NotEmpty(t, nonce)
		require.False(t, nonces[nonce], "nonce must be unique per response")
		nonces[nonce] = true
		require.Equal(t, 3, strings.Count(head, `nonce="`+nonce+`"`), path)
		require.Contains(t, csp, "'strict-dynamic'")
		require.Contains(t, csp, "object-src 'none'; base-uri 'none';")
		require.NotContains(t, csp, "'unsafe-inline'")
		require.NotContains(t, csp, "require-trusted-types-for")
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("search=missing"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "gtag('config', 'G-TEST');")

	// Settings must stay usable even if the configured snippet closes a textarea.
	escapeAttempt := `</textarea><script>window.customScriptEscape = true;</script>`
	settings = postScripts(escapeAttempt).Body.String()
	require.Contains(t, settings, template.HTMLEscapeString(escapeAttempt))
	require.NotContains(t, settings, escapeAttempt)

	postScripts("")
	loaded, err := data.GetAllSettingsOptions()
	require.NoError(t, err)
	require.Empty(t, loaded.CustomScripts)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "G-TEST")
	require.NotContains(t, rec.Body.String(), "customScriptEscape")
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "require-trusted-types-for 'script'")
	require.NotContains(t, rec.Header().Get("Content-Security-Policy"), "'strict-dynamic'")
}

func TestCustomScriptsRequiresLogin(t *testing.T) {
	withRouterTestWorkingDir(t)
	flags := newTestFlags(false, "DEFAULT", false)
	handler, err := NewRouter(&flags)
	require.NoError(t, err)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/settings/scripts", strings.NewReader("custom-scripts=untrusted"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusFound, rec.Code)
		require.Equal(t, "/settings/application", rec.Header().Get("Location"))
	}
	options, err := data.GetAllSettingsOptions()
	require.NoError(t, err)
	require.Empty(t, options.CustomScripts)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=admin"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, req)
	require.Equal(t, http.StatusFound, login.Code)
	cookies := login.Result().Cookies()
	require.NotEmpty(t, cookies)

	form := url.Values{"custom-scripts": {customScriptsFixture}}
	req = httptest.NewRequest(http.MethodPost, "/settings/scripts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	options, err = data.GetAllSettingsOptions()
	require.NoError(t, err)
	require.Equal(t, customScriptsFixture, options.CustomScripts)
}
