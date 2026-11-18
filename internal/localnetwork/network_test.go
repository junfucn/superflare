package localnetwork

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
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
	for _, tc := range []struct{ request, configured, expected string }{
		{"https://nav.example.com", "", ""},
		{"http://192.168.1.10:3636", "", "http://192.168.1.10:3636" + IdentityPath},
		{"https://nav.example.com", "https://nav.home.arpa", "https://nav.home.arpa" + IdentityPath},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.request, nil)
		r.RemoteAddr = "192.168.1.2:1234"
		got := Config(model.Application{LocalNetworkURL: tc.configured, LocalNetworkID: "test"}, r)
		if got.URL != tc.expected {
			t.Errorf("probe URL=%q, want %q", got.URL, tc.expected)
		}
	}
}
