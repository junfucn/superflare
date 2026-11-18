package home

import (
	"strings"
	"testing"

	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
)

func TestBookmarkSearchOpensOnlyMatchingSubdirectories(t *testing.T) {
	originalLoader := loadNormalBookmarks
	t.Cleanup(func() { loadNormalBookmarks = originalLoader })
	loadNormalBookmarks = func() (model.Bookmarks, error) {
		return model.Bookmarks{
			Categories: []model.Category{{ID: "1", Name: "Services"}},
			Items: []model.Bookmark{
				{Name: "Needle", URL: "https://needle.invalid", Category: "1", Subdir: "Storage"},
				{Name: "Unrelated", URL: "https://other.invalid", Category: "1", Subdir: "Other"},
			},
		}, nil
	}
	options := &model.Application{IconMode: define.IconModeHidden}
	browse, err := GenerateBookmarkTemplateErr("", options)
	if err != nil || strings.Contains(string(browse), `<details class="bookmark-subdir" open`) {
		t.Fatalf("browsing should keep subdirectories closed: %s, %v", browse, err)
	}
	search, err := GenerateBookmarkTemplateErr("needle", options)
	if err != nil || !strings.Contains(string(search), `<details class="bookmark-subdir" open><summary title="Storage"`) ||
		strings.Contains(string(search), "Unrelated") {
		t.Fatalf("search should reveal the matching bookmark: %s, %v", search, err)
	}
}
