package home

import (
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/stretchr/testify/require"
)

func TestBookmarkGroupsPreserveOrderAndOwnTheirMarkup(t *testing.T) {
	fixture := model.Bookmarks{
		Categories: []model.Category{
			{ID: "b", Name: "Second"},
			{ID: "a", Name: "First"},
			{ID: "empty", Name: "Empty"},
		},
		Items: []model.Bookmark{
			{Name: "Loose", URL: "https://example.com/loose", Favorite: true},
			{Name: "Plain", URL: "https://example.com/plain", Category: "b"},
			{Name: "Folder one", URL: "https://example.com/one", Category: " b ", Subdir: "Z"},
			{Name: "Folder two", URL: "https://example.com/two", Category: "b", Subdir: "A"},
			{Name: "Folder three", URL: "https://example.com/three", Category: "b", Subdir: "Z"},
			{Name: "Other", URL: "https://example.com/other", Category: "a"},
			{Name: "Private", URL: "https://example.com/private", Category: "b", Private: true},
			{Name: "Unknown", URL: "https://example.com/unknown", Category: "missing"},
		},
	}
	previousLoader := loadNormalBookmarks
	loadNormalBookmarks = func() (model.Bookmarks, error) { return fixture, nil }
	t.Cleanup(func() { loadNormalBookmarks = previousLoader })
	options := &model.Application{IconMode: define.IconModeHidden, Locale: "en", ShowBookmarks: true, ShowFavorites: true}
	modules, err := generateBookmarkModules("", options, false, nil, false, true, true)
	require.NoError(t, err)
	markup := string(modules.Bookmarks)
	previous := -1
	for _, label := range []string{"Second", "Folder one", "Folder three", "Folder two", "Plain", "First", "Other", "Ungrouped", "Loose", "Unknown"} {
		token := ">" + label + "<"
		require.Equal(t, 1, strings.Count(markup, token), "each item or category must appear once: %s", label)
		position := strings.Index(markup, token)
		require.Greater(t, position, previous, "unexpected order for %s", label)
		previous = position
	}
	require.NotContains(t, markup, ">Private<")
	require.NotContains(t, markup, ">Empty<")
	require.Contains(t, string(modules.Favorites), ">Loose<")

	// Reusing scratch buffers for another request must not change either of
	// the strings already returned to this request.
	originalBookmarks := strings.Clone(markup)
	originalFavorites := strings.Clone(string(modules.Favorites))
	fixture.Items[0].Name = "Changed"
	next, err := generateBookmarkModules("", options, false, nil, false, true, true)
	require.NoError(t, err)
	require.Contains(t, string(next.Favorites), ">Changed<")
	require.Equal(t, originalBookmarks, string(modules.Bookmarks))
	require.Equal(t, originalFavorites, string(modules.Favorites))
}
