package home

import (
	"html/template"
	"strings"

	"golang.org/x/net/html"

	"github.com/junfuchang/superflare/config/model"
)

func hasCustomScripts(options model.Application) bool {
	return strings.TrimSpace(options.CustomScripts) != ""
}

// Only administrator-configured snippets are trusted here. Settings forms keep
// the original string escaped, so a broken snippet cannot disable its editor.
func renderCustomScripts(raw, nonce string) template.HTML {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var b strings.Builder
	z := html.NewTokenizer(strings.NewReader(raw))
	for {
		kind := z.Next()
		original := string(z.Raw())
		if kind == html.ErrorToken {
			b.WriteString(original)
			break
		}
		if kind == html.StartTagToken {
			token := z.Token()
			if token.Data == "script" {
				attrs := token.Attr[:0]
				for _, attr := range token.Attr {
					if attr.Key != "nonce" {
						attrs = append(attrs, attr)
					}
				}
				token.Attr = append(attrs, html.Attribute{Key: "nonce", Val: nonce})
				b.WriteString(token.String())
				continue
			}
		}
		b.WriteString(original)
	}
	return template.HTML(b.String())
}
