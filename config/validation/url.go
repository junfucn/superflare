package validation

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// IsSafeNavigationURL rejects malformed URLs, browser-executable schemes and control characters.
func IsSafeNavigationURL(raw string) bool {
	if raw == "" || strings.IndexFunc(raw, unicode.IsControl) >= 0 || strings.Contains(raw, `\`) {
		return false
	}
	checkURL := strings.TrimSpace(raw)
	if checkURL == "" {
		return false
	}
	u, err := url.Parse(checkURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "vbscript", "data", "blob", "filesystem":
		return false
	default:
		return true
	}
}

// IsValidHTTPURL rejects malformed destinations, credentials and invalid ports.
func IsValidHTTPURL(raw string) bool {
	if raw == "" || strings.IndexFunc(raw, func(char rune) bool { return unicode.IsSpace(char) || unicode.IsControl(char) }) >= 0 || strings.Contains(raw, `\`) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) || u.Hostname() == "" || u.User != nil || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if strings.Contains(u.Hostname(), ":") {
		if !strings.HasPrefix(u.Host, "[") {
			return false
		}
		addr, err := netip.ParseAddr(u.Hostname())
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return false
		}
	} else if strings.HasPrefix(u.Host, "[") {
		return false
	} else if !isValidHTTPHostname(u.Hostname()) {
		return false
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return false
		}
	}
	return true
}

func isValidHTTPHostname(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Is4()
	}
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if char == '-' || char == '_' || unicode.IsLetter(char) || unicode.IsNumber(char) {
				continue
			}
			return false
		}
	}
	return true
}
