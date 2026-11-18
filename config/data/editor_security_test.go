package data

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/model"
	"github.com/stretchr/testify/require"
)

func TestEditorJSONPreservesTextWithoutClosingScript(t *testing.T) {
	input := model.Bookmark{Name: "<&Test>", URL: "https://test.invalid/?a=1&b=2", Desc: "</script><script>alert(1)</script>"}
	raw, err := jsonStringify(input)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(raw), "</script")
	var decoded model.Bookmark
	require.NoError(t, json.Unmarshal([]byte(raw), &decoded))
	require.Equal(t, input, decoded)
}

func TestEditorRoundTripKeepsUngroupedBookmarks(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, EnsureRuntimeDataFiles())
	item := model.Bookmark{Name: "Ungrouped", URL: "https://test.invalid/", LocalURL: "http://192.168.1.1/", Desc: "Description", Private: true, Favorite: true}
	require.NoError(t, SaveFavoriteBookmarks(model.Bookmarks{}))
	require.NoError(t, SaveNormalBookmarks(model.Bookmarks{Items: []model.Bookmark{item}}))
	_, raw, err := GetBookmarksForEditor()
	require.NoError(t, err)
	var rows []model.Bookmark
	require.NoError(t, json.Unmarshal([]byte(raw), &rows))
	require.Equal(t, []model.Bookmark{item}, rows)
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	require.NoError(t, w.Write([]string{"1", item.Name, item.URL, item.LocalURL, editorUngroupedCategory, "", "", item.Desc, "true", "true"}))
	w.Flush()
	require.NoError(t, w.Error())
	require.NoError(t, UpdateBookmarksFromEditor("", buf.String()))
	normal, err := LoadNormalBookmarks()
	require.NoError(t, err)
	require.Equal(t, []model.Bookmark{item}, normal.Items)
	apps, err := LoadFavoriteBookmarks()
	require.NoError(t, err)
	require.Empty(t, apps.Items)
	_, err = getCategoriesFromCSV("1," + editorUngroupedCategory)
	require.Error(t, err)
}

func TestEditorRoundTripNormalizesCategoryReferences(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, EnsureRuntimeDataFiles())
	require.NoError(t, SaveFavoriteBookmarks(model.Bookmarks{}))
	item := model.Bookmark{Name: "Grouped", URL: "https://test.invalid/", Category: " 1 ", Favorite: true, Private: true}
	require.NoError(t, SaveNormalBookmarks(model.Bookmarks{
		Categories: []model.Category{{ID: "1 ", Name: " Group "}},
		Items:      []model.Bookmark{item},
	}))
	categoriesJSON, bookmarksJSON, err := GetBookmarksForEditor()
	require.NoError(t, err)
	var categories []model.Category
	var rows []model.Bookmark
	require.NoError(t, json.Unmarshal([]byte(categoriesJSON), &categories))
	require.NoError(t, json.Unmarshal([]byte(bookmarksJSON), &rows))
	require.Equal(t, []model.Category{{ID: "1", Name: "Group"}}, categories)
	item.Category = "1"
	require.Equal(t, []model.Bookmark{item}, rows)
	cached, err := LoadNormalBookmarks()
	require.NoError(t, err)
	require.Equal(t, " 1 ", cached.Items[0].Category, "editor normalization must not modify cached source data")
	require.NoError(t, UpdateBookmarksFromEditor("1,Group", "1,Grouped,https://test.invalid/,,Group,,,,true,true"))
	normal, err := LoadNormalBookmarks()
	require.NoError(t, err)
	require.Equal(t, []model.Bookmark{item}, normal.Items)
	apps, err := LoadFavoriteBookmarks()
	require.NoError(t, err)
	require.Empty(t, apps.Items)
}

func TestEditorRoundTripKeepsCategoryIDMatchingLegacyAppMarker(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, EnsureRuntimeDataFiles())
	app := model.Bookmark{Name: "App", URL: "https://app.invalid/", Private: true}
	item := model.Bookmark{
		Name: "Bookmark", URL: "https://bookmark.invalid/",
		Category: "_FLARE_FIXED_CATEGORY", Favorite: true, Private: true,
	}
	categories := []model.Category{{ID: item.Category, Name: "Group"}}
	require.NoError(t, SaveFavoriteBookmarks(model.Bookmarks{Items: []model.Bookmark{app}}))
	require.NoError(t, SaveNormalBookmarks(model.Bookmarks{Categories: categories, Items: []model.Bookmark{item}}))
	_, raw, err := GetBookmarksForEditor()
	require.NoError(t, err)
	var rows []struct {
		model.Bookmark
		IsApplication bool
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &rows))
	require.Len(t, rows, 2)
	require.True(t, rows[0].IsApplication)
	require.Equal(t, app, rows[0].Bookmark)
	require.False(t, rows[1].IsApplication)
	require.Equal(t, item, rows[1].Bookmark)

	require.NoError(t, UpdateBookmarksFromEditor(item.Category+",Group",
		"1,App,https://app.invalid/,,"+editorFixedCategory+",,,,true,false\n"+
			"2,Bookmark,https://bookmark.invalid/,,Group,,,,true,true"))
	normal, err := LoadNormalBookmarks()
	require.NoError(t, err)
	require.Equal(t, model.Bookmarks{Categories: categories, Items: []model.Bookmark{item}}, normal)
	apps, err := LoadFavoriteBookmarks()
	require.NoError(t, err)
	require.Equal(t, []model.Bookmark{app}, apps.Items)
}
