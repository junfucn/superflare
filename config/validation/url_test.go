package validation

import "testing"

func TestIsValidHTTPURL(t *testing.T) {
	for _, raw := range []string{
		"http://example.invalid/path",
		"HTTPS://example.invalid:8443/path",
		"https://[fd00::1]:8443/path",
	} {
		if !IsValidHTTPURL(raw) {
			t.Errorf("IsValidHTTPURL(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"ftp://example.invalid/path",
		"https://user:password@example.invalid/path",
		"https://example.invalid:/path",
		"https://example.invalid:0/path",
		"https://example.invalid:65536/path",
		"https://example.invalid:bad/path",
		"https://[fe80::1%25en0]/path",
		"https://[bad]/path",
		"https://fd00::1/path",
		"https://[127.0.0.1]/path",
		"https://example.invalid/with\\backslash",
		"https://example.invalid/with\nnewline",
		"https:///missing-host",
		"https://example.invalid,evil.invalid/path",
		"https://example..invalid/path",
		"https://-example.invalid/path",
		"https://example-.invalid/path",
		"https://%65xample.invalid/path",
	} {
		if IsValidHTTPURL(raw) {
			t.Errorf("IsValidHTTPURL(%q) = true", raw)
		}
	}
}

func TestIsSafeNavigationURL(t *testing.T) {
	for _, raw := range []string{
		"https://example.invalid/path",
		"chrome-extension://abcdefghijklmnop/index.html",
		"mailto:help@example.invalid",
		"custom-app://open/item",
		"/relative/path",
		"https://example.invalid/path with spaces",
		"https://example.invalid/search?q=&percnt;value&x=1",
		" https://example.invalid/path ",
	} {
		if !IsSafeNavigationURL(raw) {
			t.Errorf("IsSafeNavigationURL(%q) = false", raw)
		}
	}
	for _, raw := range []string{
		"javascript:alert(1)",
		"",
		"   ",
		" javascript:alert(1)",
		"\tjavascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"blob:https://example.invalid/id",
		"filesystem:https://example.invalid/temporary/page.html",
		"java\tscript:alert(1)",
		"https://example.invalid/with\nnewline",
		"https://example.invalid/with\\backslash",
	} {
		if IsSafeNavigationURL(raw) {
			t.Errorf("IsSafeNavigationURL(%q) = true", raw)
		}
	}
}
