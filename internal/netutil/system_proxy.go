package netutil

import (
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

type systemProxyConfig struct {
	server string
	bypass string
}

// ProxyFromEnvironmentOrSystem uses the Windows manual proxy only when no
// environment proxy is configured for the requested scheme.
func ProxyFromEnvironmentOrSystem(req *http.Request) (*url.URL, error) {
	return proxyFromEnvironmentOrSystem(req, readSystemProxyConfig)
}

func proxyFromEnvironmentOrSystem(req *http.Request, readSystem func() systemProxyConfig) (*url.URL, error) {
	if req == nil || req.URL == nil {
		return nil, nil
	}
	config := httpproxy.FromEnvironment()
	var environmentProxy string
	switch req.URL.Scheme {
	case "http":
		environmentProxy = config.HTTPProxy
	case "https":
		environmentProxy = config.HTTPSProxy
	default:
		return nil, nil
	}
	if environmentProxy != "" {
		return config.ProxyFunc()(req.URL)
	}

	host := req.URL.Hostname()
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return nil, nil
	}
	system := readSystem()
	if bypassesSystemProxy(req.URL, system.bypass) {
		return nil, nil
	}
	proxy := systemProxyForScheme(system.server, req.URL.Scheme)
	config.HTTPProxy, config.HTTPSProxy = proxy, proxy
	// Preserve NO_PROXY even when the actual proxy comes from Windows.
	return config.ProxyFunc()(req.URL)
}

func systemProxyForScheme(server string, scheme string) string {
	for _, entry := range strings.Split(server, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		protocol, address, named := strings.Cut(entry, "=")
		if !named {
			return entry
		}
		if strings.EqualFold(strings.TrimSpace(protocol), scheme) {
			return strings.TrimSpace(address)
		}
	}
	return ""
}

func bypassesSystemProxy(target *url.URL, bypass string) bool {
	host := strings.ToLower(target.Hostname())
	for _, entry := range strings.Split(bypass, ";") {
		pattern := strings.ToLower(strings.TrimSpace(entry))
		if pattern == "<local>" && !strings.ContainsAny(host, ".:") {
			return true
		}
		if matched, _ := path.Match(pattern, host); matched {
			return true
		}
		if matched, _ := path.Match(pattern, strings.ToLower(target.Host)); matched {
			return true
		}
	}
	return false
}
