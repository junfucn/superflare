package data

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEditorRevisionRejectsStaleDraftsAndSupportsRepeatedSaves(t *testing.T) {
	t.Chdir(t.TempDir())
	const categories = "1,Group"
	const bookmarks = "1,First,https://first.invalid/,,Group,,,,false,true"
	require.NoError(t, UpdateBookmarksFromEditor(categories, bookmarks))
	initial, err := GetEditorSnapshot()
	require.NoError(t, err)
	require.Len(t, initial.Revision, 64)

	firstSave := "1,First saved in A,https://first.invalid/,,Group,,,,false,true"
	revision, err := UpdateBookmarksFromEditorWithRevision(categories, firstSave, initial.Revision)
	require.NoError(t, err)
	require.NotEqual(t, initial.Revision, revision)
	saved, err := GetEditorSnapshot()
	require.NoError(t, err)
	require.Equal(t, saved.Revision, revision)
	require.Contains(t, saved.Bookmarks, "First saved in A")

	for _, stale := range []string{initial.Revision, ""} {
		_, err = UpdateBookmarksFromEditorWithRevision(categories, bookmarks, stale)
		require.ErrorIs(t, err, ErrEditorRevisionConflict)
		current, err := GetEditorSnapshot()
		require.NoError(t, err)
		require.Equal(t, saved, current, "a rejected draft must not change either file")
	}
	revision, err = UpdateBookmarksFromEditorWithRevision(categories, bookmarks, revision)
	require.NoError(t, err)
	current, err := GetEditorSnapshot()
	require.NoError(t, err)
	require.Equal(t, current.Revision, revision, "a successful save must return the next usable revision")
}

func TestEditorRevisionSerializesConcurrentSaves(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, UpdateBookmarksFromEditor("", ""))
	initial, err := GetEditorSnapshot()
	require.NoError(t, err)
	type result struct {
		revision string
		err      error
	}
	results := make(chan result, 2)
	for _, name := range []string{"A", "B"} {
		go func() {
			revision, err := UpdateBookmarksFromEditorWithRevision("", "1,"+name+",https://example.invalid/,,,,,,false,false", initial.Revision)
			results <- result{revision, err}
		}()
	}
	var winner string
	conflicts := 0
	for range 2 {
		result := <-results
		if errors.Is(result.err, ErrEditorRevisionConflict) {
			conflicts++
		} else {
			require.NoError(t, result.err)
			require.Empty(t, winner, "only one concurrent draft may be saved")
			winner = result.revision
		}
	}
	require.Equal(t, 1, conflicts)
	current, err := GetEditorSnapshot()
	require.NoError(t, err)
	require.Equal(t, winner, current.Revision)
}

func TestEditorRevisionDetectsExternalChangesToEitherFile(t *testing.T) {
	for _, name := range []string{"apps", "bookmarks"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			const categories = "1,Group"
			const bookmarks = "1,First,https://app.invalid/,,,,,,false,false\n2,First,https://bookmark.invalid/,,Group,,,,false,false"
			require.NoError(t, UpdateBookmarksFromEditor(categories, bookmarks))
			initial, err := GetEditorSnapshot()
			require.NoError(t, err)
			_, err = LoadFavoriteBookmarks()
			require.NoError(t, err)
			_, err = LoadNormalBookmarks()
			require.NoError(t, err)

			path := name + ".yml"
			info, err := os.Stat(path)
			require.NoError(t, err)
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			changed := bytes.ReplaceAll(raw, []byte("First"), []byte("Other"))
			require.NotEqual(t, raw, changed)
			require.NoError(t, os.WriteFile(path, changed, info.Mode()))
			require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))

			_, err = UpdateBookmarksFromEditorWithRevision(categories, bookmarks, initial.Revision)
			require.ErrorIs(t, err, ErrEditorRevisionConflict)
			current, err := GetEditorSnapshot()
			require.NoError(t, err)
			require.NotEqual(t, initial.Revision, current.Revision)
			require.Contains(t, current.Bookmarks, "Other", "the revision must describe the actual editor data, even with a primed file cache")
			preserved, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, changed, preserved)
		})
	}
}
