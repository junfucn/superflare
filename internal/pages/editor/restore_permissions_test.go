package editor

import (
	"os"
	"runtime"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/stretchr/testify/require"
)

func TestRestorePreservesPOSIXPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not represented by Windows chmod")
	}
	t.Chdir(t.TempDir())
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, os.Chmod("config.yml", 0644))
	require.NoError(t, os.WriteFile("apps.yml", []byte("links: []\n"), 0600))
	files := map[string][]byte{
		"config": []byte("Theme: blackboard\nLoginUser: admin\nLoginPass: restored-password\n"),
		"apps":   []byte("links: []\n"),
	}
	require.NoError(t, writeRestoreFilesAtomically(files))
	for _, name := range []string{"config.yml", "apps.yml"} {
		info, err := os.Stat(name)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0600), info.Mode().Perm(), name)
	}
	require.NoError(t, os.Chmod("config.yml", 0400))
	require.NoError(t, writeRestoreFilesAtomically(files))
	info, err := os.Stat("config.yml")
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0400), info.Mode().Perm())
}
