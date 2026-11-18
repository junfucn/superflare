package data

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type permissionTestInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (i permissionTestInfo) Mode() os.FileMode { return i.mode }

func TestConfigFileModePolicy(t *testing.T) {
	original := osStat
	t.Cleanup(func() { osStat = original })
	for _, tc := range []struct {
		name     string
		existing os.FileMode
		missing  bool
		want     os.FileMode
	}{
		{"config.yml", 0644, false, 0600},
		{"config.yaml", 0400, false, 0400},
		{".env", 0600, false, 0600},
		{".env", 0644, false, 0600},
		{".env", 0, true, 0600},
		{"config.yml", 0, true, 0600},
		{"apps.yml", 0600, false, 0600},
		{"bookmarks.yml", 0400, false, 0400},
		{"ports.yaml", 0, true, 0644},
	} {
		osStat = func(string) (os.FileInfo, error) {
			if tc.missing {
				return nil, os.ErrNotExist
			}
			return permissionTestInfo{mode: tc.existing}, nil
		}
		mode, err := ConfigFileMode(filepath.Join("test", tc.name))
		if err != nil || mode != tc.want {
			t.Errorf("%s mode=%#o, err=%v, want=%#o", tc.name, mode, err, tc.want)
		}
	}
	osStat = func(string) (os.FileInfo, error) { return nil, os.ErrPermission }
	if _, err := ConfigFileMode("config.yml"); err == nil {
		t.Fatal("stat errors must not silently widen permissions")
	}
}

func TestCredentialAndEditorWritesPreservePOSIXPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not represented by Windows chmod")
	}
	t.Chdir(t.TempDir())
	if err := EnsureAppConfigExists(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".env", []byte("FLARE_USER=admin\nFLARE_PASS=old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := UpdateLoginConfig("admin", "new-password"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"config.yml", ".env"} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s must be owner-only after saving: %v, %v", path, info, err)
		}
	}
	if err := os.WriteFile("apps.yml", []byte("links: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateBookmarksFromEditor("", ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat("apps.yml")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("editor widened existing permissions: %v, %v", info, err)
	}
	if err := os.Chmod("config.yml", 0400); err != nil {
		t.Fatal(err)
	}
	if err := UpdateLoginConfig("admin", "another-password"); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat("config.yml")
	if err != nil || info.Mode().Perm() != 0400 {
		t.Fatalf("save widened owner permissions: %v, %v", info, err)
	}
}
