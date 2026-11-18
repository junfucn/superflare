package validation

import "testing"

func TestBuildSearchURL(t *testing.T) {
	for _, invalid := range []string{
		"example.invalid/search?q=%s",
		"ftp://example.invalid/search?q=%s",
		"https://[bad/search?q=%s",
		"https://example.invalid:65536/search?q=%s",
		"https://example.invalid:/search?q=%s",
		"https://user:password@example.invalid/search?q=%s",
		"https://example.invalid/search?q=%s\\unsafe",
		"https://%s.example.invalid/search",
		"https://example.invalid/search?q=%xy&s=%s",
		"https://example.invalid/search",
	} {
		if target, err := BuildSearchURL(invalid, "example"); err == nil {
			t.Errorf("accepted invalid template %q as %q", invalid, target)
		}
	}

	const want = "https://[fd00::1]:8080/find/a%20b%2Fc?q=a+b%2Fc#a%20b%2Fc"
	if got, err := BuildSearchURL("https://[fd00::1]:8080/find/%s?q=%s#%s", "a b/c"); err != nil || got != want {
		t.Fatalf("target = %q, err = %v; want %q", got, err, want)
	}
}
