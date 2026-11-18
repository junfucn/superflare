package fn

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/junfuchang/superflare/internal/netutil"
)

func TestParseRequestURL_HTTP(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://example.com:8080/foo/bar", nil)
	r.Host = "example.com:8080"
	r.RemoteAddr = "192.168.1.88:54123"
	ParseRequestURL(r)
	if RequestURL.Host != "example.com:8080" {
		t.Errorf("Host: got %q", RequestURL.Host)
	}
	if RequestURL.Hostname != "example.com" {
		t.Errorf("Hostname: got %q", RequestURL.Hostname)
	}
	if RequestURL.Port != "8080" {
		t.Errorf("Port: got %q", RequestURL.Port)
	}
	if RequestURL.Protocol != "http:" {
		t.Errorf("Protocol: got %q", RequestURL.Protocol)
	}
	if RequestURL.Pathname != "/foo/bar" {
		t.Errorf("Pathname: got %q", RequestURL.Pathname)
	}
	if RequestURL.RemoteHost != "192.168.1.88" {
		t.Errorf("RemoteHost: got %q", RequestURL.RemoteHost)
	}
}

func TestDynamicURLIPv6AndProxyScheme(t *testing.T) {
	for _, tc := range []struct {
		address, proto, hostname, port, origin, expanded string
	}{
		{"http://[fd00::1]:3636/path?q=1", "", "[fd00::1]", "3636", "http://[fd00::1]:3636", "http://[fd00::1]:5000/"},
		{"http://[fd00::1]/path?q=1", "https", "[fd00::1]", "443", "https://[fd00::1]", "https://[fd00::1]:5000/"},
		{"http://nav.example.test/path?q=1", "https", "nav.example.test", "443", "https://nav.example.test", "https://nav.example.test:5000/"},
		{"http://nav.example.test:9443/path?q=1", "https", "nav.example.test", "9443", "https://nav.example.test:9443", "https://nav.example.test:5000/"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.address, nil)
			r.RemoteAddr = "127.0.0.1:1234"
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			info := ParseRequestURLTo(r)
			if info.Hostname != tc.hostname || info.Port != tc.port || info.Origin != tc.origin || info.Href != tc.origin+"/path?q=1" {
				t.Fatalf("unexpected dynamic URL: %+v", info)
			}
			expanded := ParseDynamicUrlWith("{protocol}//{hostname}:5000/", &info)
			if expanded != tc.expanded {
				t.Fatalf("expanded=%q, want %q", expanded, tc.expanded)
			}
			parsed, err := url.Parse(expanded)
			if err != nil || parsed.Port() != "5000" {
				t.Fatalf("invalid expanded URL: %s, %v", expanded, err)
			}
		})
	}
}

func TestDynamicURLUsesForwardedHostOnlyFromTrustedProxy(t *testing.T) {
	for _, tc := range []struct {
		name, peer, forwardedHost, wantHost, wantOrigin string
	}{
		{"trusted", "127.0.0.1:1234", "nav.example.test:9443", "nav.example.test:9443", "https://nav.example.test:9443"},
		{"untrusted", "93.184.216.34:1234", "attacker.example.test", "internal.example.test", "http://internal.example.test"},
		{"unsafe", "127.0.0.1:1234", "attacker.example.test/path", "internal.example.test", "https://internal.example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://internal.example.test/path", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-Host", tc.forwardedHost)
			r.Header.Set("X-Forwarded-Proto", "https")
			if tc.name == "untrusted" {
				r = netutil.WithTrustedProxies(r, netutil.TrustedProxies{})
			} else {
				proxies, err := netutil.ParseTrustedProxies("")
				if err != nil {
					t.Fatal(err)
				}
				r = netutil.WithTrustedProxies(r, proxies)
			}
			info := ParseRequestURLTo(r)
			if info.Host != tc.wantHost || info.Origin != tc.wantOrigin {
				t.Fatalf("Host=%q Origin=%q, want %q %q", info.Host, info.Origin, tc.wantHost, tc.wantOrigin)
			}
		})
	}
}

func TestParseRequestURL_HTTPS(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	r.Host = "example.com"
	r.TLS = &tls.ConnectionState{}
	ParseRequestURL(r)
	if RequestURL.Protocol != "https:" {
		t.Errorf("Protocol: got %q", RequestURL.Protocol)
	}
	if RequestURL.Port != "443" {
		t.Errorf("Port: got %q", RequestURL.Port)
	}
}

func TestParseRequestURL_NoPort(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	r.Host = "example.com"
	ParseRequestURL(r)
	if RequestURL.Hostname != "example.com" || RequestURL.Port != "80" {
		t.Errorf("Hostname=%q Port=%q", RequestURL.Hostname, RequestURL.Port)
	}
}

func TestParseDynamicUrl(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "http://localhost:3636/", nil)
	r.Host = "localhost:3636"
	ParseRequestURL(r)
	out := ParseDynamicUrl("origin={origin} host={host} path={pathname}")
	if out != "origin=http://localhost:3636 host=localhost:3636 path=/" {
		t.Errorf("ParseDynamicUrl: got %q", out)
	}
	out2 := ParseDynamicUrl("no placeholders")
	if out2 != "no placeholders" {
		t.Errorf("ParseDynamicUrl no placeholders: got %q", out2)
	}
}

func TestHostLooksLocalNetwork(t *testing.T) {
	tests := map[string]bool{
		"localhost:3636":         true,
		"127.0.0.1:3636":         true,
		"192.168.1.20:3636":      true,
		"10.0.0.3":               true,
		"[fd00::1]:3636":         true,
		"fnos.local":             true,
		"nas":                    true,
		"example.com":            false,
		"superflare.example.com": false,
		"":                       false,
	}
	for host, expected := range tests {
		if got := HostLooksLocalNetwork(host); got != expected {
			t.Fatalf("HostLooksLocalNetwork(%q): expected %v, got %v", host, expected, got)
		}
	}
}

func TestLocalHostsMayShareNetwork(t *testing.T) {
	tests := []struct {
		name       string
		accessHost string
		localHost  string
		expected   bool
	}{
		{name: "same ipv4 /24", accessHost: "192.168.1.20:3636", localHost: "192.168.1.50", expected: true},
		{name: "different ipv4 /24", accessHost: "192.168.0.10", localHost: "192.168.10.1", expected: false},
		{name: "different private ranges", accessHost: "192.168.0.10", localHost: "10.0.0.2", expected: false},
		{name: "localhost access is unknown", accessHost: "localhost:3636", localHost: "192.168.10.1", expected: true},
		{name: "hostname access is unknown", accessHost: "nas.local", localHost: "192.168.10.1", expected: true},
		{name: "hostname local is unknown", accessHost: "192.168.0.10", localHost: "nas.local", expected: true},
		{name: "public local target is not local", accessHost: "192.168.0.10", localHost: "8.8.8.8", expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LocalHostsMayShareNetwork(tc.accessHost, tc.localHost); got != tc.expected {
				t.Fatalf("LocalHostsMayShareNetwork(%q, %q): expected %v, got %v", tc.accessHost, tc.localHost, tc.expected, got)
			}
		})
	}
}

func TestLocalURLMayShareNetworkWithHost(t *testing.T) {
	if LocalURLMayShareNetworkWithHost("192.168.0.10:3636", "http://192.168.10.1:8080/app") {
		t.Fatal("expected different LAN network URL to be rejected")
	}
	if !LocalURLMayShareNetworkWithHost("192.168.0.10:3636", "http://192.168.0.20:8080/app") {
		t.Fatal("expected same LAN network URL to be accepted")
	}
}

func TestRequestClientHostUsesForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "10.1.2.3:54321"
	req.Header.Set("X-Forwarded-For", "192.168.2.77, 10.0.0.1")
	proxies, err := netutil.ParseTrustedProxies("10.1.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	req = netutil.WithTrustedProxies(req, proxies)
	if got := RequestClientHost(req); got != "192.168.2.77" {
		t.Fatalf("expected first forwarded client host, got %q", got)
	}
}

func TestRequestLooksLocalNetworkUsesForwardedClient(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://superflare.example.com/", nil)
	req.Host = "superflare.example.com"
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "192.168.2.77")
	proxies, err := netutil.ParseTrustedProxies("")
	if err != nil {
		t.Fatal(err)
	}
	req = netutil.WithTrustedProxies(req, proxies)
	if !RequestLooksLocalNetwork(req) {
		t.Fatal("expected forwarded private client address to look local")
	}
}

func TestRequestClientHostIgnoresForwardedHeadersFromUntrustedPeers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.RemoteAddr = "93.184.216.34:54321"
	req.Header.Set("X-Forwarded-For", "192.168.2.77")
	req.Header.Set("X-Real-IP", "10.0.0.9")
	if got := RequestClientHost(req); got != "93.184.216.34" {
		t.Fatalf("untrusted forwarded headers changed client host to %q", got)
	}
}

func TestLocalURLMayShareNetworkWithRequestUsesRemoteHost(t *testing.T) {
	info := &DynamicURL{Hostname: "superflare.example.com", RemoteHost: "192.168.2.77"}
	if !LocalURLMayShareNetworkWithRequest(info, "http://192.168.2.10/app") {
		t.Fatal("expected local URL to match forwarded client network")
	}
	if LocalURLMayShareNetworkWithRequest(info, "http://192.168.10.10/app") {
		t.Fatal("expected different forwarded client network to be rejected")
	}
}

func TestLocalURLMayShareNetworkWithRequestUsesLocalInterfacesForLocalhost(t *testing.T) {
	orig := readInterfaceAddrs
	readInterfaceAddrs = func() ([]net.Addr, error) {
		_, cidr, err := net.ParseCIDR("192.168.0.10/24")
		if err != nil {
			t.Fatalf("ParseCIDR: %v", err)
		}
		return []net.Addr{cidr}, nil
	}
	defer func() { readInterfaceAddrs = orig }()

	info := &DynamicURL{Hostname: "localhost", RemoteHost: "127.0.0.1"}
	if !LocalURLMayShareNetworkWithRequest(info, "http://192.168.0.20/app") {
		t.Fatal("expected localhost access to use matching server interface")
	}
	if LocalURLMayShareNetworkWithRequest(info, "http://192.168.10.20/app") {
		t.Fatal("expected localhost access to reject different server interface network")
	}
}

func TestLocalURLMayShareNetworkWithRequestKeepsUnknownDomainConservative(t *testing.T) {
	info := &DynamicURL{Hostname: "superflare.example.com", RemoteHost: "127.0.0.1"}
	if !LocalURLMayShareNetworkWithRequest(info, "http://192.168.10.20/app") {
		t.Fatal("expected public domain with proxy loopback to stay conservative")
	}
}
