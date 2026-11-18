package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/stretchr/testify/require"
)

func TestLifecycleConfigCommandsPreserveCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, os.WriteFile(".env", []byte("FLARE_COOKIE_SECRET=unchanged\n"), 0600))
	user := `user'"with\quotes`
	for _, pass := range []string{
		"'secret123", `with"quotes`, `"""triple"""`, `back\slash\`,
		"`backtick", "#; pass'word", "%(FLARE_USER)s-secret",
		strings.Repeat("word ", 40) + "last", "\u5bc6\u7801'\"",
	} {
		t.Run(pass, func(t *testing.T) {
			var output bytes.Buffer
			handled, err := RunConfigCommand([]string{"config-login"}, strings.NewReader(user+"\x00"+pass+"\x00"), &output)
			require.True(t, handled)
			require.NoError(t, err)
			require.Empty(t, output.String())
			for _, tc := range []struct{ format, file, key, want string }{
				{"env", ".env", "FLARE_USER", user}, {"env", ".env", "FLARE_PASS", pass},
				{"yaml", "config.yml", "LoginUser", user}, {"yaml", "config.yml", "LoginPass", pass},
			} {
				output.Reset()
				handled, err := RunConfigCommand([]string{"config-read", tc.format, tc.file, tc.key}, nil, &output)
				require.True(t, handled)
				require.NoError(t, err)
				require.Equal(t, tc.want, output.String())
			}
		})
	}
}

func TestLifecycleConfigCommandsRejectInvalidInput(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, input := range []string{"", "user\x00pass", "user\x00pass\x00extra\x00", "user\x00bad\npass\x00", strings.Repeat("u", data.LoginUserMaxBytes+1) + "\x00pass\x00"} {
		var output bytes.Buffer
		handled, err := RunConfigCommand([]string{"config-login"}, strings.NewReader(input), &output)
		require.True(t, handled)
		require.Error(t, err)
		require.Empty(t, output.String())
		require.NoFileExists(t, "config.yml")
	}
	for _, tc := range []struct{ format, content string }{
		{"yaml", "LoginPass: [secret"}, {"env", "FLARE_PASS=\"\"\"secret"},
	} {
		require.NoError(t, os.WriteFile("broken", []byte(tc.content), 0600))
		var output bytes.Buffer
		handled, err := RunConfigCommand([]string{"config-read", tc.format, "broken", "LoginPass"}, nil, &output)
		require.True(t, handled)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		require.Empty(t, output.String())
	}
}
