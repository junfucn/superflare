package editor

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junfuchang/superflare/internal/netutil"
)

func TestEditorPortHostUsesLANEntry(t *testing.T) {
	proxies, err := netutil.ParseTrustedProxies("192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, origin, requestURL, forwardedHost, want string
		trustedPeer                                   bool
	}{
		{name: "configured IPv6 takes priority", origin: "https://[fd00::11]:3636", requestURL: "http://172.18.0.2:3636", want: "[fd00::11]"},
		{name: "direct LAN entry", requestURL: "http://nas.home.arpa:3636", want: "nas.home.arpa"},
		{name: "trusted proxy with LAN entry", requestURL: "http://172.18.0.2:3636", forwardedHost: "nas.home.arpa", trustedPeer: true, want: "nas.home.arpa"},
		{name: "trusted proxy with public entry", requestURL: "http://172.18.0.2:3636", forwardedHost: "nav.example.com", trustedPeer: true},
		{name: "untrusted proxy header", requestURL: "http://nav.example.com", forwardedHost: "nas.home.arpa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.requestURL, nil)
			req.RemoteAddr = "192.0.2.10:12345"
			req.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			if tc.trustedPeer {
				req = netutil.WithTrustedProxies(req, proxies)
			}
			if got := editorPortHost(tc.origin, req); got != tc.want {
				t.Fatalf("editorPortHost = %q, want %q", got, tc.want)
			}
		})
	}
}
