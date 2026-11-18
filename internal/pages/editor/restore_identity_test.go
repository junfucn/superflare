package editor

import (
	"errors"
	"os"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/stretchr/testify/require"
)

func TestRestoreIdentityUpgradeRollsBackWithConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, data.EnsureLocalNetworkIdentity())
	before, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	refresh := refreshPagePaletteCache
	t.Cleanup(func() { refreshPagePaletteCache = refresh })
	refreshPagePaletteCache = func() error { return errors.New("forced palette failure") }
	require.ErrorContains(t, writeRestoreFilesAtomically(map[string][]byte{
		"config": []byte("Title: Legacy\nTheme: blackboard\nLoginUser: admin\nLoginPass: admin\n"),
	}), "forced palette failure")
	after, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)
}
