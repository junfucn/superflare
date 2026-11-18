package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrustedProxiesConfigSources(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("FLARE_TRUSTED_PROXIES", "10.0.0.2")
	flags, err := ParseEnvVarsE()
	require.NoError(t, err)
	require.Equal(t, "10.0.0.2", flags.TrustedProxies)
	require.NoError(t, os.WriteFile(".env", []byte("FLARE_USER=admin\nFLARE_PASS=test-password\nFLARE_TRUSTED_PROXIES=10.1.0.0/24\n"), 0600))
	flags, err = ParseEnvFileE(flags)
	require.NoError(t, err)
	require.Equal(t, "10.1.0.0/24", flags.TrustedProxies)
	previousArgs := os.Args
	t.Cleanup(func() { os.Args = previousArgs })
	os.Args = []string{"superflare", "--trusted_proxies=none"}
	cliFlags, fs, err := GetCliFlags()
	require.NoError(t, err)
	flags, err = resolveCLIFlags(flags, cliFlags, fs)
	require.NoError(t, err)
	require.Equal(t, "none", flags.TrustedProxies)
}
