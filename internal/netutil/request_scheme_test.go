package netutil

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestSchemeTrustsOnlyConfiguredPeers(t *testing.T) {
	for _, tc := range []struct {
		name, setting, peer, forwardedProto, forwarded, want string
		tls                                                  bool
	}{
		{name: "loopback", peer: "127.0.0.1:1234", forwardedProto: "https", want: "https"},
		{name: "IPv6 loopback", peer: "[::1]:1234", forwarded: `for=unknown;proto="https"`, want: "https"},
		{name: "untrusted public peer", peer: "93.184.216.34:1234", forwardedProto: "https", want: "http"},
		{name: "untrusted private peer", peer: "192.168.1.20:1234", forwardedProto: "https", want: "http"},
		{name: "configured proxy", setting: "192.168.1.20", peer: "192.168.1.20:1234", forwardedProto: " HTTPS , http", want: "https"},
		{name: "configured network", setting: "10.0.0.0/24", peer: "10.0.0.8:1234", forwarded: "proto=https, proto=http", want: "https"},
		{name: "configured IPv6", setting: "fd00::/64", peer: "[fd00::8]:1234", forwardedProto: "https", want: "https"},
		{name: "outside configured network", setting: "10.0.0.0/24", peer: "10.0.1.8:1234", forwardedProto: "https", want: "http"},
		{name: "disabled", setting: "none", peer: "127.0.0.1:1234", forwardedProto: "https", want: "http"},
		{name: "invalid scheme", peer: "127.0.0.1:1234", forwardedProto: "javascript", want: "http"},
		{name: "do not downgrade direct TLS", peer: "93.184.216.34:1234", forwardedProto: "http", tls: true, want: "https"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxies, err := ParseTrustedProxies(tc.setting)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, "http://nav.example.test/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
			r.Header.Set("Forwarded", tc.forwarded)
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if got := RequestScheme(WithTrustedProxies(r, proxies)); got != tc.want {
				t.Fatalf("scheme=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseTrustedProxiesRejectsInvalidSettings(t *testing.T) {
	for _, raw := range []string{"*", "proxy.example.com", "127.0.0.1,", "10.0.0.0/99", "none,127.0.0.1", "::ffff:10.0.0.0/104"} {
		if _, err := ParseTrustedProxies(raw); err == nil {
			t.Errorf("invalid proxy setting accepted: %q", raw)
		}
	}
}

func TestRequestForwardedHeadersTrustOnlySocketPeer(t *testing.T) {
	proxies, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, peer, forwardedHost, forwardedFor, realIP, wantHost, wantIP string
	}{
		{"configured proxy", "10.1.2.3:1234", "nav.example.test:9443", "192.168.1.20, 10.2.3.4", "10.9.8.7", "nav.example.test:9443", "192.168.1.20"},
		{"untrusted peer", "192.168.1.2:1234", "attacker.example.test", "192.168.1.20", "10.9.8.7", "", ""},
		{"invalid client IP", "10.1.2.3:1234", "", "attacker, 192.168.1.20", "", "", ""},
		{"fallback to real IP", "10.1.2.3:1234", "", "attacker", "10.9.8.7", "", "10.9.8.7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://internal.example.test/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			r.Header.Set("X-Forwarded-For", tc.forwardedFor)
			r.Header.Set("X-Real-IP", tc.realIP)
			r = WithTrustedProxies(r, proxies)
			if got := RequestForwardedHost(r); got != tc.wantHost {
				t.Errorf("forwarded host=%q, want %q", got, tc.wantHost)
			}
			gotIP := RequestForwardedClientIP(r)
			got := ""
			if gotIP.IsValid() {
				got = gotIP.String()
			}
			if got != tc.wantIP {
				t.Errorf("forwarded client IP=%q, want %q", got, tc.wantIP)
			}
		})
	}
}
