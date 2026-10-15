package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/junfuchang/superflare/config/model"
)

func TestReadFileCachedReloadsWhenFileChangesWithSameSize(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "config.yml")

	if err := os.WriteFile(filePath, []byte("a"), 0644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	initialTime := time.Unix(1700000000, 0)
	if err := os.Chtimes(filePath, initialTime, initialTime); err != nil {
		t.Fatalf("set initial modtime: %v", err)
	}

	first, err := readFileCached(filePath, func() ([]byte, error) { return os.ReadFile(filePath) })
	if err != nil {
		t.Fatalf("read initial cache: %v", err)
	}
	if string(first) != "a" {
		t.Fatalf("unexpected initial content: %q", string(first))
	}

	if err := os.WriteFile(filePath, []byte("b"), 0644); err != nil {
		t.Fatalf("write updated file: %v", err)
	}
	updatedTime := initialTime.Add(2 * time.Second)
	if err := os.Chtimes(filePath, updatedTime, updatedTime); err != nil {
		t.Fatalf("set updated modtime: %v", err)
	}

	second, err := readFileCached(filePath, func() ([]byte, error) { return os.ReadFile(filePath) })
	if err != nil {
		t.Fatalf("read updated cache: %v", err)
	}
	if string(second) != "b" {
		t.Fatalf("expected updated content after external file change, got %q", string(second))
	}
}

func TestReadParsedFileCachedReusesAndInvalidatesSnapshots(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "config.yml")
	modified := time.Unix(1700000000, 0)
	write := func(raw string) {
		t.Helper()
		modified = modified.Add(time.Second)
		if err := os.WriteFile(filePath, []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(filePath, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	parses := 0
	parse := func(raw []byte) (string, error) {
		parses++
		return string(raw), nil
	}
	read := func(want string, wantParses int) {
		t.Helper()
		got, err := readParsedFileCached(filePath, "test", parse)
		if err != nil || got != want || parses != wantParses {
			t.Fatalf("snapshot=%q err=%v parses=%d, want %q and %d parses", got, err, parses, want, wantParses)
		}
	}
	write("a")
	read("a", 1)
	read("a", 1)
	write("b")
	read("b", 2)
	invalidateFileCachePath(filePath)
	read("b", 3)
}

func TestBookmarkSnapshotsAreIsolatedFromCallerMutations(t *testing.T) {
	t.Chdir(t.TempDir())
	original := model.Bookmarks{
		Categories: []model.Category{{ID: "main", Name: "Main"}},
		Items:      []model.Bookmark{{Name: "Original", URL: "https://example.com", Category: "main", Private: true}},
	}
	if err := SaveNormalBookmarks(original); err != nil {
		t.Fatal(err)
	}
	first, err := LoadNormalBookmarks()
	if err != nil {
		t.Fatal(err)
	}
	first.Items[0].Name = "Changed by caller"
	first.Items[0].Private = false
	first.Categories[0].Name = "Changed category"
	second, err := LoadNormalBookmarks()
	if err != nil {
		t.Fatal(err)
	}
	if second.Items[0] != original.Items[0] || second.Categories[0] != original.Categories[0] {
		t.Fatalf("caller mutated the cached snapshot: %+v", second)
	}
	second.Items[0].Name = "Saved update"
	if err := SaveNormalBookmarks(second); err != nil {
		t.Fatal(err)
	}
	updated, err := LoadNormalBookmarks()
	if err != nil || updated.Items[0].Name != "Saved update" {
		t.Fatalf("save did not invalidate the snapshot: %+v, %v", updated, err)
	}
	if err := os.WriteFile("bookmarks.yml", []byte("links:\n- name: invalid\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNormalBookmarks(); err == nil {
		t.Fatal("invalid external edit must not return the old validated snapshot")
	}
}

func TestSettingsSnapshotKeepsRawAndNormalizedValuesSeparate(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("config.yml", []byte("Title: Original\nLocale: en-US\nTheme: blackboard\n"), 0644); err != nil {
		t.Fatal(err)
	}
	settings, err := GetAllSettingsOptions()
	if err != nil || settings.Locale != "en" || settings.IconMode != "FILLING" {
		t.Fatalf("normalized settings=%+v, err=%v", settings, err)
	}
	settings.Title = "Changed by caller"
	raw, err := loadAppConfigFromYaml("config")
	if err != nil || raw.Locale != "en-US" || raw.Title != "Original" || raw.IconMode != "" {
		t.Fatalf("raw config was polluted by normalized settings: %+v, %v", raw, err)
	}
	again, err := GetAllSettingsOptions()
	if err != nil || again.Title != "Original" {
		t.Fatalf("settings were polluted by caller: %+v, %v", again, err)
	}
}
