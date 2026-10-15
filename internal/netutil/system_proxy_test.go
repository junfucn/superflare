package netutil

import (
	"net/http"
	"testing"
)

func TestProxyFromEnvironmentOrSystem(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(name, "")
	}
	tests := []struct {
		name, target, environmentProxy, noProxy, server, bypass, want string
	}{
		{name: "Windows shared proxy", target: "https://wallhaven.cc/favicon.ico", server: "127.0.0.1:10808", want: "http://127.0.0.1:10808"},
		{name: "Windows HTTPS proxy", target: "https://wallhaven.cc/favicon.ico", server: "http=127.0.0.1:8080;https=127.0.0.1:8443", want: "http://127.0.0.1:8443"},
		{name: "empty proxy entries", target: "https://wallhaven.cc/favicon.ico", server: ";http=127.0.0.1:8080;;https=127.0.0.1:8443;", want: "http://127.0.0.1:8443"},
		{name: "Windows HTTP proxy", target: "http://public.test/icon", server: "http=127.0.0.1:8080;https=127.0.0.1:8443", want: "http://127.0.0.1:8080"},
		{name: "no matching protocol", target: "https://public.test/icon", server: "http=127.0.0.1:8080"},
		{name: "environment takes precedence", target: "https://wallhaven.cc/favicon.ico", environmentProxy: "http://127.0.0.1:9999", server: "127.0.0.1:10808", want: "http://127.0.0.1:9999"},
		{name: "environment NO_PROXY", target: "https://wallhaven.cc/favicon.ico", environmentProxy: "http://127.0.0.1:9999", noProxy: "wallhaven.cc", server: "127.0.0.1:10808"},
		{name: "system respects NO_PROXY", target: "https://wallhaven.cc/favicon.ico", noProxy: "wallhaven.cc", server: "127.0.0.1:10808"},
		{name: "system wildcard bypass", target: "https://nas.example.test/icon", server: "127.0.0.1:10808", bypass: "*.example.test"},
		{name: "system port bypass", target: "http://nas.example.test:8080/icon", server: "127.0.0.1:10808", bypass: "nas.example.test:8080"},
		{name: "system local bypass", target: "http://nas/icon", server: "127.0.0.1:10808", bypass: "<local>"},
		{name: "private IPv4 stays direct", target: "http://192.168.1.20/icon", server: "127.0.0.1:10808"},
		{name: "private IPv6 stays direct", target: "http://[fd00::1]/icon", server: "127.0.0.1:10808"},
		{name: "localhost stays direct", target: "http://localhost/icon", server: "127.0.0.1:10808"},
		{name: "no configured proxy", target: "https://wallhaven.cc/favicon.ico"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HTTP_PROXY", tt.environmentProxy)
			t.Setenv("HTTPS_PROXY", tt.environmentProxy)
			t.Setenv("NO_PROXY", tt.noProxy)
			systemReads := 0
			proxy, err := proxyFromEnvironmentOrSystem(&http.Request{URL: mustParseURL(t, tt.target)}, func() systemProxyConfig {
				systemReads++
				return systemProxyConfig{server: tt.server, bypass: tt.bypass}
			})
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if proxy != nil {
				got = proxy.String()
			}
			if got != tt.want {
				t.Fatalf("proxy = %q, want %q", got, tt.want)
			}
			if tt.environmentProxy != "" && systemReads != 0 {
				t.Fatal("an explicit environment proxy must not consult Windows settings")
			}
		})
	}
}
