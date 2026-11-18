package data

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnsureRuntimeDataFilesCreatesMissingFiles(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := EnsureRuntimeDataFiles(); err != nil {
		t.Fatalf("EnsureRuntimeDataFiles: %v", err)
	}

	for _, name := range []string{"apps.yml", "bookmarks.yml", "ports.yaml"} {
		target := filepath.Join(tmpDir, name)
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("expected %s to exist: %v", name, err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		want, err := readDefaultFile(name)
		if err != nil {
			t.Fatalf("read default %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s should match repository defaults", name)
		}
	}
}

func TestEnsureAppConfigExistsCreatesMissingFile(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := EnsureAppConfigExists(); err != nil {
		t.Fatalf("EnsureAppConfigExists: %v", err)
	}
	target := filepath.Join(tmpDir, "config.yml")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected config.yml to exist: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read config.yml: %v", err)
	}
	want, err := readDefaultFile("config.yml")
	if err != nil {
		t.Fatalf("read default config.yml: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("config.yml should match repository defaults")
	}
}

func TestInitializationWaitsForConfigReplacement(t *testing.T) {
	for _, tc := range []struct {
		file string
		init func() error
	}{
		{"config.yml", EnsureAppConfigExists},
		{"apps.yml", EnsureRuntimeDataFiles},
		{"bookmarks.yml", EnsureRuntimeDataFiles},
		{"ports.yaml", EnsureRuntimeDataFiles},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Chdir(t.TempDir())
			require.NoError(t, EnsureAppConfigExists())
			require.NoError(t, EnsureRuntimeDataFiles())
			original := []byte("# existing operator configuration\n")
			require.NoError(t, os.WriteFile(tc.file, original, 0600))
			done := make(chan error, 1)
			observedGap := make(chan struct{}, 1)
			originalStat := osStat
			osStat = func(path string) (os.FileInfo, error) {
				info, err := originalStat(path)
				if filepath.Base(path) == tc.file && os.IsNotExist(err) {
					select {
					case observedGap <- struct{}{}:
					default:
					}
				}
				return info, err
			}
			defer func() { osStat = originalStat }()
			checkedDuringWrite := false
			require.NoError(t, withConfigWriteLock(func() error {
				if err := os.Rename(tc.file, tc.file+".backup"); err != nil {
					return err
				}
				go func() { done <- tc.init() }()
				select {
				case <-observedGap:
					checkedDuringWrite = true
				case <-time.After(50 * time.Millisecond):
				}
				return os.Rename(tc.file+".backup", tc.file)
			}))
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("initialization did not finish after the writer released its lock")
			}
			require.False(t, checkedDuringWrite, "initialization inspected the temporary replacement gap")
			got, err := os.ReadFile(tc.file)
			require.NoError(t, err)
			require.Equal(t, original, got)
		})
	}
}
