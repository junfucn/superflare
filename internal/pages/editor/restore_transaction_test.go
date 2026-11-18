package editor

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/stretchr/testify/require"
)

func TestRestoreKeepsLiveFilesReadableDuringReplacementAndRollback(t *testing.T) {
	for _, failure := range []string{"replacement", "palette refresh"} {
		t.Run(failure, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, data.EnsureAppConfigExists())
			require.NoError(t, data.EnsureRuntimeDataFiles())
			before := map[string][]byte{}
			for _, name := range []string{"apps.yml", "config.yml"} {
				raw, err := os.ReadFile(name)
				require.NoError(t, err)
				before[name] = raw
			}
			originalRename, originalRefresh := renameRestoreFile, refreshPagePaletteCache
			t.Cleanup(func() {
				renameRestoreFile, refreshPagePaletteCache = originalRename, originalRefresh
			})
			replacements, rollbacks := 0, 0
			renameRestoreFile = func(oldPath, newPath string) error {
				for name := range before {
					_, err := os.ReadFile(name)
					require.NoError(t, err, "live restore target disappeared")
				}
				_, err := data.GetAllSettingsOptions()
				require.NoError(t, err, "config reader failed during restore")
				if strings.Contains(filepath.Base(oldPath), ".backup-") {
					rollbacks++
				} else {
					replacements++
					if failure == "replacement" && filepath.Base(newPath) == "config.yml" {
						return errors.New("forced replacement failure")
					}
				}
				return originalRename(oldPath, newPath)
			}
			if failure == "palette refresh" {
				refreshPagePaletteCache = func() error { return errors.New("forced palette refresh failure") }
			}
			err := writeRestoreFilesAtomically(map[string][]byte{
				"apps":   []byte("links: []\n"),
				"config": []byte("Title: Restored\nTheme: blackboard\nLoginUser: restored\nLoginPass: restored-password\n"),
			})
			require.ErrorContains(t, err, "forced")
			require.Equal(t, 2, replacements)
			require.GreaterOrEqual(t, rollbacks, 1)
			for name, raw := range before {
				after, err := os.ReadFile(name)
				require.NoError(t, err)
				require.Equal(t, raw, after, name)
			}
			for _, pattern := range []string{".*.restore-*", ".*.backup-*"} {
				matches, err := filepath.Glob(pattern)
				require.NoError(t, err)
				require.Empty(t, matches)
			}
			options, err := data.GetAllSettingsOptions()
			require.NoError(t, err)
			require.NotEqual(t, "Restored", options.Title, "rollback left stale parsed config")
		})
	}
}

func TestInvalidRestoreLeavesConfigBookmarksAndEnvUnchanged(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, data.EnsureRuntimeDataFiles())
	require.NoError(t, os.WriteFile(".env", []byte("FLARE_USER=owner\nFLARE_PASS=strong-password\n"), 0600))
	require.NoError(t, data.UpdateLoginConfig("owner", "strong-password"))
	before := map[string][]byte{}
	for _, name := range []string{"config.yml", "apps.yml", "bookmarks.yml", ".env"} {
		raw, err := os.ReadFile(name)
		require.NoError(t, err)
		before[name] = raw
	}
	large := []byte("Title: Legacy\nTheme: blackboard\nFutureSetting: ")
	large = append(large, bytes.Repeat([]byte{'x'}, data.MaxConfigFileSize-len(large)-1)...)
	large = append(large, '\n')
	for _, tc := range []struct {
		name string
		raw  []byte
		want string
	}{
		{"partial account", []byte("Theme: blackboard\nLoginUser: restored\n"), "credentials are incomplete"},
		{"legacy upgrade exceeds limit", large, "8 MiB limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := writeRestoreFilesAtomically(map[string][]byte{"apps": []byte("links: []\n"), "config": tc.raw})
			require.ErrorContains(t, err, tc.want)
			for name, raw := range before {
				after, err := os.ReadFile(name)
				require.NoError(t, err)
				require.Equal(t, raw, after, name)
			}
		})
	}
	_, err := buildBackupArchive()
	require.NoError(t, err)
}
