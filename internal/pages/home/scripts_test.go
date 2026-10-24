package home

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderCustomScriptsPreservesCodeAndAttributes(t *testing.T) {
	const code = "\nconst html = '<script data-test=\"&\">';\nif (1 < 2 && true) { window.test = '{{unchanged}}'; }\n"
	raw := `<!-- analytics --><SCRIPT NONCE="old" nonce="duplicate" async src="https://example.com/a.js?x=1&amp;y=2" data-test="&quot;">` + code + `</SCRIPT>`
	got := string(renderCustomScripts(raw, "current-nonce"))
	require.Contains(t, got, "<!-- analytics -->")
	require.Contains(t, got, code)
	require.Contains(t, got, `src="https://example.com/a.js?x=1&amp;y=2"`)
	require.Contains(t, got, `data-test="&#34;"`)
	require.Contains(t, got, `async=""`)
	require.Equal(t, 1, strings.Count(got, `nonce=`))
	require.Contains(t, got, `nonce="current-nonce"`)
	require.Empty(t, renderCustomScripts(" \n\t", "current-nonce"))
}
