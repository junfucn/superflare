package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEditorRejectsOversizedConfigWithoutChangingEitherFile(t *testing.T) {
	for _, category := range []string{editorFixedCategory, editorUngroupedCategory} {
		t.Run(category, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, EnsureRuntimeDataFiles())
			before := make(map[string][]byte)
			for _, name := range []string{"apps.yml", "bookmarks.yml"} {
				raw, err := os.ReadFile(name)
				require.NoError(t, err)
				before[name] = raw
			}
			csv := "1,Large,https://example.invalid/,," + category + ",,," + strings.Repeat("x", MaxConfigFileSize) + ",false,false"
			require.ErrorContains(t, UpdateBookmarksFromEditor("", csv), "8 MiB limit")
			for name, raw := range before {
				after, err := os.ReadFile(name)
				require.NoError(t, err)
				require.Equal(t, raw, after, name)
			}
			for _, pattern := range []string{".*.tmp-*", ".*.backup-*"} {
				entries, err := filepath.Glob(pattern)
				require.NoError(t, err)
				require.Empty(t, entries)
			}
		})
	}
}

func TestConfigSaveSizeLimitMatchesRestoreBoundary(t *testing.T) {
	t.Chdir(t.TempDir())
	exact := []byte(strings.Repeat("x", MaxConfigFileSize))
	require.NoError(t, saveFile("apps.yml", exact))
	require.ErrorContains(t, saveFile("apps.yml", append(exact, 'x')), "8 MiB limit")
	after, err := os.ReadFile("apps.yml")
	require.NoError(t, err)
	require.Equal(t, exact, after)
}

func TestSettingsRejectOversizedConfigBeforeReplacingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, EnsureAppConfigExists())
	before, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	options, err := GetAllSettingsOptions()
	require.NoError(t, err)
	options.CustomScripts = strings.Repeat("x", MaxConfigFileSize)
	require.ErrorContains(t, saveAppConfigToYamlFile("config", options), "8 MiB limit")
	after, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)
}
