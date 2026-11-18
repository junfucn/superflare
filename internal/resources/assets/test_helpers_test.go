package assets

import (
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/fn"
)

func configureTestIcons(t *testing.T, sources ...string) {
	t.Helper()
	if err := data.EnsureRuntimeDataFiles(); err != nil {
		t.Fatal(err)
	}
	items := make([]model.Bookmark, 0, len(sources))
	for _, source := range sources {
		items = append(items, model.Bookmark{Name: "Test icon", URL: source, Icon: source})
	}
	if err := data.SaveFavoriteBookmarks(model.Bookmarks{Items: items}); err != nil {
		t.Fatal(err)
	}
}

func fnGetwdForAssetsTest() func() (string, error) {
	return fn.TestGetwdExportForAssets()
}

func setFnGetwdForAssetsTest(next func() (string, error)) {
	fn.TestSetGetwdExportForAssets(next)
}

func restoreFnGetwdForAssetsTest(original func() (string, error)) {
	fn.TestSetGetwdExportForAssets(original)
}
