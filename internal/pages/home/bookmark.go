package home

import (
	"bytes"
	"html/template"
	"sort"
	"strconv"
	"strings"

	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/fn"
	"github.com/junfuchang/superflare/internal/pool"
)

type bookmarkModules struct {
	Bookmarks                 template.HTML
	Favorites                 template.HTML
	HasFavorites              bool
	HasDescriptions           bool
	BookmarksHaveDescriptions bool
	FavoritesHaveDescriptions bool
	items                     []model.Bookmark
	favoriteItems             []model.Bookmark
}

func bookmarkVisible(item model.Bookmark, canViewPrivate bool) bool {
	return canViewPrivate || !item.Private
}

func GenerateBookmarkTemplateErr(filter string, options *model.Application) (template.HTML, error) {
	return GenerateBookmarkTemplateWithLocalAndURLErr(filter, options, false, nil)
}

func GenerateBookmarkTemplateWithLocalErr(filter string, options *model.Application, preferLocal bool) (template.HTML, error) {
	return GenerateBookmarkTemplateWithLocalAndURLErr(filter, options, preferLocal, nil)
}

func GenerateBookmarkTemplateWithLocalAndURLErr(filter string, options *model.Application, preferLocal bool, requestURL *fn.DynamicURL) (template.HTML, error) {
	modules, err := generateBookmarkModulesWithLocalAndURLErr(filter, options, preferLocal, requestURL, true)
	return modules.Bookmarks, err
}

func generateBookmarkModulesWithLocalAndURLErr(filter string, options *model.Application, preferLocal bool, requestURL *fn.DynamicURL, canViewPrivate bool) (bookmarkModules, error) {
	return generateBookmarkModules(filter, options, preferLocal, requestURL, canViewPrivate, true, true)
}

func generateBookmarkModules(filter string, options *model.Application, preferLocal bool, requestURL *fn.DynamicURL, canViewPrivate bool, showBookmarks bool, showFavorites bool) (bookmarkModules, error) {
	if !showBookmarks && !showFavorites {
		return bookmarkModules{}, nil
	}
	if options == nil {
		options = &model.Application{}
	}
	bookmarksData, err := loadNormalBookmarks()
	if err != nil {
		return bookmarkModules{}, err
	}
	b := pool.GetBuffer()
	defer pool.PutBuffer(b)

	filterLower := strings.ToLower(filter)
	bookmarks := make([]model.Bookmark, 0, len(bookmarksData.Items))
	favoriteCount := 0
	bookmarksHaveDescriptions := false
	favoritesHaveDescriptions := false
	for _, bookmark := range bookmarksData.Items {
		if !bookmarkVisible(bookmark, canViewPrivate) {
			continue
		}
		bookmark.URL = fn.ParseDynamicUrlWith(bookmark.URL, requestURL)
		bookmark.LocalURL = fn.ParseDynamicUrlWith(bookmark.LocalURL, requestURL)
		if filter != "" &&
			!strings.Contains(strings.ToLower(bookmark.Name), filterLower) &&
			!strings.Contains(strings.ToLower(bookmark.URL), filterLower) &&
			!strings.Contains(strings.ToLower(bookmark.LocalURL), filterLower) &&
			!strings.Contains(strings.ToLower(bookmark.Desc), filterLower) &&
			!strings.Contains(strings.ToLower(bookmark.Subdir), filterLower) {
			continue
		}
		bookmarks = append(bookmarks, bookmark)
		hasDescription := strings.TrimSpace(bookmark.Desc) != ""
		bookmarksHaveDescriptions = bookmarksHaveDescriptions || hasDescription
		if bookmark.Favorite {
			favoriteCount++
			favoritesHaveDescriptions = favoritesHaveDescriptions || hasDescription
		}
	}
	var favorites []model.Bookmark
	if favoriteCount > 0 {
		favorites = make([]model.Bookmark, 0, favoriteCount)
		for _, bookmark := range bookmarks {
			if bookmark.Favorite {
				favorites = append(favorites, bookmark)
			}
		}
	}
	if showFavorites {
		sort.SliceStable(favorites, func(i, j int) bool {
			left := strings.ToLower(strings.TrimSpace(favorites[i].Name))
			right := strings.ToLower(strings.TrimSpace(favorites[j].Name))
			if left != right {
				return left < right
			}
			return favorites[i].Name < favorites[j].Name
		})
	}

	if showBookmarks && len(bookmarksData.Categories) > 0 {
		// Partition once instead of scanning every bookmark for every category.
		groups := make(map[string][]model.Bookmark, len(bookmarksData.Categories)+1)
		for _, category := range bookmarksData.Categories {
			groups[strings.TrimSpace(category.ID)] = nil
		}
		for _, bookmark := range bookmarks {
			key := strings.TrimSpace(bookmark.Category)
			if _, known := groups[key]; !known {
				key = ""
			}
			groups[key] = append(groups[key], bookmark)
		}
		b.WriteString(`<div class="bookmark-groups">`)
		for _, category := range bookmarksData.Categories {
			renderBookmarksWithCategories(b, groups[strings.TrimSpace(category.ID)], category, filter != "", options.OpenBookmarkNewTab, options.EnableEncryptedLink, options.IconMode, preferLocal, requestURL)
		}
		renderUngroupedBookmarks(b, groups[""], options.Locale, options.OpenBookmarkNewTab, options.EnableEncryptedLink, options.IconMode, preferLocal, requestURL)
		b.WriteString(`</div>`)
	} else if showBookmarks {
		b.WriteString(`<div class="bookmark-groups">`)
		renderBookmarksWithoutCategories(b, bookmarks, options.OpenBookmarkNewTab, options.EnableEncryptedLink, options.IconMode, preferLocal, requestURL)
		b.WriteString(`</div>`)
	}

	var favoritesHTML template.HTML
	if showFavorites && len(favorites) > 0 {
		favoritesBuilder := pool.GetBuffer()
		favoritesBuilder.WriteString(`<div class="bookmark-groups">`)
		renderBookmarksWithoutCategories(favoritesBuilder, favorites, options.OpenBookmarkNewTab, options.EnableEncryptedLink, options.IconMode, preferLocal, requestURL)
		favoritesBuilder.WriteString(`</div>`)
		favoritesHTML = template.HTML(favoritesBuilder.String())
		pool.PutBuffer(favoritesBuilder)
	}
	hasDescriptions := (options.ShowBookmarks && bookmarksHaveDescriptions) ||
		(options.ShowFavorites && len(favorites) > 0 && favoritesHaveDescriptions)

	return bookmarkModules{
		Bookmarks:                 template.HTML(b.String()),
		Favorites:                 favoritesHTML,
		HasFavorites:              len(favorites) > 0,
		HasDescriptions:           hasDescriptions,
		BookmarksHaveDescriptions: bookmarksHaveDescriptions,
		FavoritesHaveDescriptions: favoritesHaveDescriptions,
		items:                     bookmarks,
		favoriteItems:             favorites,
	}, nil
}

func renderBookmarksWithoutCategories(b *bytes.Buffer, bookmarks []model.Bookmark, OpenBookmarkNewTab bool, EnableEncryptedLink bool, IconMode string, preferLocal bool, requestURL *fn.DynamicURL) {
	b.WriteString(`<div class="bookmark-group-container pull-left"><ul class="bookmark-list">`)
	for _, bookmark := range bookmarks {
		renderBookmarkItem(b, bookmark, OpenBookmarkNewTab, EnableEncryptedLink, IconMode, preferLocal, requestURL)
	}
	b.WriteString(`</ul></div>`)
}

func renderBookmarksWithCategories(b *bytes.Buffer, bookmarks []model.Bookmark, category model.Category, expandSubdirs bool, OpenBookmarkNewTab bool, EnableEncryptedLink bool, IconMode string, preferLocal bool, requestURL *fn.DynamicURL) {
	if len(bookmarks) == 0 {
		return
	}
	subdirs := make(map[string][]model.Bookmark)
	subdirOrder := make([]string, 0)
	for _, bookmark := range bookmarks {
		if bookmark.Subdir != "" {
			if _, ok := subdirs[bookmark.Subdir]; !ok {
				subdirOrder = append(subdirOrder, bookmark.Subdir)
			}
			subdirs[bookmark.Subdir] = append(subdirs[bookmark.Subdir], bookmark)
		}
	}
	b.WriteString(`<div class="bookmark-group-container pull-left"><h3 class="bookmark-group-title">`)
	b.WriteString(template.HTMLEscapeString(category.Name))
	b.WriteString(`</h3>`)
	if len(subdirOrder) > 0 {
		b.WriteString(`<div class="bookmark-subdirs">`)
		for idx, subdir := range subdirOrder {
			id := "subdir-" + category.ID + "-" + strconv.Itoa(idx)
			b.WriteString(`<details class="bookmark-subdir"`)
			if expandSubdirs {
				b.WriteString(` open`)
			}
			b.WriteString(`><summary title="`)
			b.WriteString(template.HTMLEscapeString(subdir))
			b.WriteString(`">`)
			b.WriteString(template.HTMLEscapeString(subdir))
			b.WriteString(`</summary><ul class="bookmark-list" id="`)
			b.WriteString(template.HTMLEscapeString(id))
			b.WriteString(`">`)
			for _, bookmark := range subdirs[subdir] {
				renderBookmarkItem(b, bookmark, OpenBookmarkNewTab, EnableEncryptedLink, IconMode, preferLocal, requestURL)
			}
			b.WriteString(`</ul></details>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`<ul class="bookmark-list">`)
	for _, bookmark := range bookmarks {
		if bookmark.Subdir == "" {
			renderBookmarkItem(b, bookmark, OpenBookmarkNewTab, EnableEncryptedLink, IconMode, preferLocal, requestURL)
		}
	}
	b.WriteString(`</ul></div>`)
}

func renderUngroupedBookmarks(b *bytes.Buffer, bookmarks []model.Bookmark, locale string, OpenBookmarkNewTab bool, EnableEncryptedLink bool, IconMode string, preferLocal bool, requestURL *fn.DynamicURL) {
	if len(bookmarks) == 0 {
		return
	}
	b.WriteString(`<div class="bookmark-group-container pull-left"><h3 class="bookmark-group-title">`)
	b.WriteString(template.HTMLEscapeString(localeUngroupedTitle(locale)))
	b.WriteString(`</h3><ul class="bookmark-list">`)
	for _, bookmark := range bookmarks {
		renderBookmarkItem(b, bookmark, OpenBookmarkNewTab, EnableEncryptedLink, IconMode, preferLocal, requestURL)
	}
	b.WriteString(`</ul></div>`)
}

func localeUngroupedTitle(locale string) string {
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		return "Ungrouped"
	}
	return "未分类"
}

func renderBookmarkItem(b *bytes.Buffer, bookmark model.Bookmark, OpenBookmarkNewTab bool, EnableEncryptedLink bool, IconMode string, preferLocal bool, requestURL *fn.DynamicURL) {
	templateURL := renderBookmarkHref(bookmark.URL, bookmark.LocalURL, preferLocal, EnableEncryptedLink, requestURL)
	templateIcon := renderBookmarkIcon(bookmark.Icon, bookmark.URL, IconMode)
	b.WriteString(`<li>`)
	b.WriteString(`<a`)
	renderBookmarkNavigationAttributes(b, templateURL, OpenBookmarkNewTab)
	b.WriteString(` class="bookmark"`)
	renderNetworkAttributes(b, bookmark.URL, bookmark.LocalURL, EnableEncryptedLink, requestURL)
	if description := strings.TrimSpace(bookmark.Desc); description != "" {
		b.WriteString(` data-bookmark-description="`)
		b.WriteString(template.HTMLEscapeString(description))
		b.WriteString(`"`)
	}
	b.WriteString(`>`)
	b.WriteString(templateIcon)
	b.WriteString(`<span class="bookmark-label">`)
	b.WriteString(template.HTMLEscapeString(bookmark.Name))
	b.WriteString(`</span></a></li>`)
}
