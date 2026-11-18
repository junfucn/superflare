package netutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type TrustedProxies struct {
	prefixes []netip.Prefix
}

type trustedProxiesKey struct{}

// An empty setting trusts local reverse proxies only; "none" disables trust.
func ParseTrustedProxies(raw string) (TrustedProxies, error) {
	result := TrustedProxies{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return result, nil
	}
	result.prefixes = make([]netip.Prefix, 0)
	if strings.EqualFold(raw, "none") {
		return result, nil
	}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if ip, err := netip.ParseAddr(item); err == nil && ip.Zone() == "" {
			ip = ip.Unmap()
			result.prefixes = append(result.prefixes, netip.PrefixFrom(ip, ip.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil || prefix.Addr().Is4In6() {
			return TrustedProxies{}, fmt.Errorf("invalid trusted proxy IP or CIDR: %q", item)
		}
		result.prefixes = append(result.prefixes, prefix.Masked())
	}
	return result, nil
}

func WithTrustedProxies(r *http.Request, proxies TrustedProxies) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), trustedProxiesKey{}, proxies))
}

// RequestForwardedHost ignores forwarding headers from untrusted socket peers.
// RequestForwardedHost shares the socket-peer trust policy with RequestScheme.
func RequestForwardedHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	proxies, _ := r.Context().Value(trustedProxiesKey{}).(TrustedProxies)
	if !proxies.contains(r.RemoteAddr) {
		return ""
	}
	first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	return strings.TrimSpace(first)
}

// RequestForwardedClientIP accepts client address headers only from a trusted
// socket peer. Proxies must replace these headers rather than append to them.
func RequestForwardedClientIP(r *http.Request) netip.Addr {
	if r == nil {
		return netip.Addr{}
	}
	proxies, _ := r.Context().Value(trustedProxiesKey{}).(TrustedProxies)
	if !proxies.contains(r.RemoteAddr) {
		return netip.Addr{}
	}
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		value := strings.TrimSpace(r.Header.Get(header))
		if header == "X-Forwarded-For" {
			value, _, _ = strings.Cut(value, ",")
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(value))
		if err == nil && ip.Zone() == "" {
			return ip.Unmap()
		}
	}
	return netip.Addr{}
}

func (p TrustedProxies) contains(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	if p.prefixes == nil {
		return ip.IsLoopback()
	}
	for _, prefix := range p.prefixes {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

// RequestScheme uses forwarding headers only from the configured socket peer.
// Proxies must replace these headers instead of accepting client-supplied values.
func RequestScheme(r *http.Request) string {
	if r == nil {
		return "http"
	}
	proxies, _ := r.Context().Value(trustedProxiesKey{}).(TrustedProxies)
	if proxies.contains(r.RemoteAddr) {
		first, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
		if scheme := normalizedScheme(first); scheme != "" {
			return scheme
		}
		first, _, _ = strings.Cut(r.Header.Get("Forwarded"), ",")
		for _, part := range strings.Split(first, ";") {
			key, value, ok := strings.Cut(part, "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "proto") {
				if scheme := normalizedScheme(strings.Trim(strings.TrimSpace(value), `"`)); scheme != "" {
					return scheme
				}
			}
		}
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func normalizedScheme(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "http":
		return "http"
	case "https":
		return "https"
	default:
		return ""
	}
}
