package home

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/fn"
	"golang.org/x/net/html"
)

func TestRenderBookmarkHrefPrefersLocalURL(t *testing.T) {
	requestURL := &fn.DynamicURL{Hostname: "192.168.1.20"}
	href := renderBookmarkHref("https://public.example.com/app", "http://192.168.1.10/app", true, false, requestURL)
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatalf("parse href: %v", err)
	}
	if parsed.Path != define.MiscPages.RedirLocal.Path {
		t.Fatalf("expected local redir path, got %q", parsed.Path)
	}
	source, err := data.Base64DecodeUrl(parsed.Query().Get("go"))
	if err != nil {
		t.Fatalf("decode source: %v", err)
	}
	local, err := data.Base64DecodeUrl(parsed.Query().Get("local"))
	if err != nil {
		t.Fatalf("decode local: %v", err)
	}
	if string(source) != "https://public.example.com/app" || string(local) != "http://192.168.1.10/app" {
		t.Fatalf("unexpected redirect params: source=%q local=%q", source, local)
	}
}

func TestRenderBookmarkHrefChecksIdentityFromPublicNetwork(t *testing.T) {
	href := renderBookmarkHref("https://public.example.com/app", "http://192.168.1.10/app", false, false, nil)
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != define.MiscPages.RedirLocal.Path {
		t.Fatalf("expected browser identity check, got %q", href)
	}
}

func TestRenderBookmarkHrefAllowsRoutedLocalNetwork(t *testing.T) {
	requestURL := &fn.DynamicURL{Hostname: "192.168.0.10"}
	href := renderBookmarkHref("https://public.example.com/app", "http://192.168.10.1/app", true, false, requestURL)
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != define.MiscPages.RedirLocal.Path {
		t.Fatalf("expected identity check for routed network, got %q", href)
	}
}

func TestRenderBookmarkHrefDoesNotGateOnForwardedClientSubnet(t *testing.T) {
	requestURL := &fn.DynamicURL{Hostname: "superflare.example.com", RemoteHost: "192.168.0.88"}
	href := renderBookmarkHref("https://public.example.com/app", "http://192.168.10.1/app", true, false, requestURL)
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != define.MiscPages.RedirLocal.Path {
		t.Fatalf("expected browser identity check, got %q", href)
	}
}

func TestRenderBookmarkHrefKeepsEncryptedSourceWhenNoLocalURL(t *testing.T) {
	href := renderBookmarkHref("https://public.example.com/app", "", true, true, nil)
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatalf("parse href: %v", err)
	}
	if parsed.Path != define.MiscPages.RedirHelper.Path {
		t.Fatalf("expected encrypted redir path, got %q", parsed.Path)
	}
}

func TestRenderBookmarkHrefDoesNotUseLocalRedirWhenSourceURLIsNonHTTP(t *testing.T) {
	href := renderBookmarkHref("chrome-extension://abc/index.html", "http://192.168.1.10/app", true, false, nil)
	parsed, err := url.Parse(href)
	if err != nil {
		t.Fatalf("parse href: %v", err)
	}
	if parsed.Path != define.MiscPages.RedirHelper.Path {
		t.Fatalf("expected helper redir path, got %q", parsed.Path)
	}
	source, err := data.Base64DecodeUrl(parsed.Query().Get("go"))
	if err != nil {
		t.Fatalf("decode source: %v", err)
	}
	if string(source) != "chrome-extension://abc/index.html" {
		t.Fatalf("unexpected source redirect param: %q", source)
	}
}

func TestRenderBookmarkHrefRejectsExecutableSchemes(t *testing.T) {
	for _, rawURL := range []string{
		"javascript:alert(1)",
		"JaVaScRiPt:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"blob:https://example.invalid/id",
		"filesystem:https://example.invalid/temporary/page.html",
		"java\tscript:alert(1)",
		"https://example.invalid/\npath",
	} {
		if got := renderBookmarkHref(rawURL, "", false, true, nil); got != "" {
			t.Errorf("renderBookmarkHref(%q) = %q; want disabled link", rawURL, got)
		}
	}
}

func TestRenderBookmarkHrefPreservesNonExecutableSchemes(t *testing.T) {
	for _, rawURL := range []string{
		"mailto:help@example.invalid",
		"custom-app://open/item",
	} {
		if got := renderBookmarkHref(rawURL, "", false, false, nil); got != rawURL {
			t.Errorf("renderBookmarkHref(%q) = %q; want original URL", rawURL, got)
		}
	}
	for _, rawURL := range []string{
		"chrome-extension://abcdefghijklmnop/index.html",
		"CHROME-EXTENSION://abcdefghijklmnop/index.html",
	} {
		if got := renderBookmarkHref(rawURL, "", false, false, nil); got == "" || got == rawURL {
			t.Errorf("chrome extension URL should keep the existing helper redirect, got %q", got)
		}
	}
}

func TestRenderBookmarkItemDisablesExecutableURL(t *testing.T) {
	var output bytes.Buffer
	renderBookmarkItem(&output, model.Bookmark{Name: "Unsafe", URL: "javascript:alert(1)"}, true, true, define.IconModeHidden, false, nil)
	got := output.String()
	if strings.Contains(got, "href=") || !strings.Contains(got, `tabindex="0" role="link" aria-disabled="true"`) || !strings.Contains(got, "Unsafe") {
		t.Fatalf("unsafe bookmark should remain visible but non-interactive: %s", got)
	}
}

func TestRenderApplicationItemDisablesExecutableURL(t *testing.T) {
	var output bytes.Buffer
	options := &model.Application{OpenAppNewTab: true, IconMode: define.IconModeHidden}
	renderApplicationItem(&output, model.Bookmark{Name: "Unsafe", URL: "javascript:alert(1)"}, options, false, nil)
	got := output.String()
	if strings.Contains(got, "href=") || strings.Contains(got, `target="_blank"`) || !strings.Contains(got, `tabindex="0" role="link" aria-disabled="true"`) || !strings.Contains(got, "Unsafe") {
		t.Fatalf("unsafe application should remain visible but non-interactive: %s", got)
	}
}

func TestRenderNetworkAttributesIgnoreUnsafeSourceURL(t *testing.T) {
	var output bytes.Buffer
	renderNetworkAttributes(&output, "javascript:alert(1)", "http://192.168.1.10/app", true, nil)
	if got := output.String(); got != "" {
		t.Fatalf("unsafe source must not opt into local network links: %s", got)
	}
}

func TestRenderedNavigationPreservesEscapedURLAttributes(t *testing.T) {
	for _, source := range []string{
		"https://example.invalid/search?q=&percnt;value&x=1",
		"javascript&colon;alert(1)",
		"java&#x73;cript:alert(1)",
	} {
		for _, application := range []bool{false, true} {
			var output bytes.Buffer
			item := model.Bookmark{Name: "Test", URL: source}
			if application {
				renderApplicationItem(&output, item, &model.Application{IconMode: define.IconModeHidden}, false, nil)
			} else {
				renderBookmarkItem(&output, item, false, false, define.IconModeHidden, false, nil)
			}
			tokens := html.NewTokenizer(strings.NewReader(output.String()))
			found := false
			for token := tokens.Next(); token != html.ErrorToken; token = tokens.Next() {
				if token != html.StartTagToken {
					continue
				}
				anchor := tokens.Token()
				if anchor.Data != "a" {
					continue
				}
				for _, attr := range anchor.Attr {
					if attr.Key == "href" {
						found = true
						if attr.Val != source {
							t.Fatalf("application=%t: href=%q, want literal URL %q", application, attr.Val, source)
						}
					}
				}
			}
			if !found {
				t.Fatalf("application=%t: missing escaped href for %q", application, source)
			}
		}
	}
}

func TestEncryptedBookmarkPreservesOriginalTarget(t *testing.T) {
	const source = " https://example.invalid/path "
	target, err := url.Parse(renderBookmarkHref(source, "", false, true, nil))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := data.Base64DecodeUrl(target.Query().Get("go"))
	if err != nil || string(decoded) != source {
		t.Fatalf("encrypted target=%q, err=%v; want original saved URL %q", decoded, err, source)
	}
}
