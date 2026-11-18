package localnetwork

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/netutil"
	"github.com/labstack/echo/v5"
)

func TestIdentityEndpointIsReadableWithoutCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := data.EnsureAppConfigExists(); err != nil {
		t.Fatal(err)
	}
	if err := data.EnsureLocalNetworkIdentity(); err != nil {
		t.Fatal(err)
	}
	options, err := data.GetAllSettingsOptions()
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	RegisterRouting(e)
	for _, method := range []string{http.MethodOptions, http.MethodGet} {
		r := httptest.NewRequest(method, "http://192.168.1.10:3636"+IdentityPath, nil)
		r.Header.Set("Origin", "https://nav.example.com")
		r.Header.Set("Access-Control-Request-Private-Network", "true")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatalf("invalid probe headers: %v", w.Header())
		}
		if method == http.MethodOptions {
			if w.Code != 204 || w.Header().Get("Access-Control-Allow-Private-Network") != "true" {
				t.Fatal("preflight failed")
			}
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(body) != 2 || body["service"] != "superflare" || body["id"] != options.LocalNetworkID {
			t.Fatalf("unexpected public identity payload: %v", body)
		}
	}
}

func TestProbeOriginNeverUsesPublicRequestOrClientIPAsEvidence(t *testing.T) {
	proxies, err := netutil.ParseTrustedProxies("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, request, configured, forwarded, expected string
		trusted                                        bool
	}{
		{name: "public entry", request: "https://nav.example.com"},
		{name: "private entry", request: "http://192.168.1.10:3636", expected: "http://192.168.1.10:3636" + IdentityPath},
		{name: "configured origin", request: "https://nav.example.com", configured: "http://192.168.1.10:3636", expected: "http://192.168.1.10:3636" + IdentityPath},
		{name: "public proxy entry", request: "http://172.18.0.2:3636", forwarded: "nav.example.com", trusted: true},
		{name: "private proxy entry", request: "http://nav.example.com", forwarded: "nas.home.arpa:8443", trusted: true, expected: "https://nas.home.arpa:8443" + IdentityPath},
		{name: "untrusted forwarded entry", request: "https://nav.example.com", forwarded: "nas.home.arpa:8443"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.request, nil)
			r.RemoteAddr = "192.168.1.2:1234"
			r.Header.Set("X-Forwarded-Host", tc.forwarded)
			r.Header.Set("X-Forwarded-Proto", "https")
			if tc.trusted {
				r.RemoteAddr = "192.0.2.10:1234"
				r = netutil.WithTrustedProxies(r, proxies)
			}
			got := Config(model.Application{LocalNetworkURL: tc.configured, LocalNetworkID: "test"}, r)
			if got.URL != tc.expected {
				t.Errorf("probe URL=%q, want %q", got.URL, tc.expected)
			}
		})
	}
}
