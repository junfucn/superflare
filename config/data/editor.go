package data

import (
	"crypto/sha256"
	"encoding/csv"
	"errors"
	"fmt"
	"gopkg.in/yaml.v2"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/junfuchang/superflare/config/model"
)

const editorFixedCategory = "[SuperFlare \u5e94\u7528]"
const editorUngroupedCategory = "[SuperFlare \u672a\u5206\u7c7b]"

type editorBookmark struct {
	model.Bookmark
	IsApplication bool
}

var editorDataRenamePath = os.Rename

var ErrEditorRevisionConflict = errors.New("editor data has changed since it was loaded")

type EditorSnapshot struct {
	Categories string
	Bookmarks  string
	Revision   string
}

func GetBookmarksForEditor() (categories string, bookmarks string, err error) {
	snapshot, err := GetEditorSnapshot()
	return snapshot.Categories, snapshot.Bookmarks, err
}

func GetEditorSnapshot() (snapshot EditorSnapshot, err error) {
	err = withConfigWriteLock(func() error {
		appsRaw, bookmarksRaw, err := readEditorBookmarkFiles()
		if err != nil {
			return err
		}
		snapshot.Categories, snapshot.Bookmarks, err = editorBookmarksJSON(appsRaw, bookmarksRaw)
		if err != nil {
			return err
		}
		snapshot.Revision = editorBookmarkRevision(appsRaw, bookmarksRaw)
		return nil
	})
	return snapshot, err
}

// Read and hash the same bytes that populate the editor, under the save lock.
func readEditorBookmarkFiles() (appsRaw, bookmarksRaw []byte, err error) {
	appsPath, err := configPath("apps")
	if err != nil {
		return nil, nil, err
	}
	appsRaw, err = readFile(appsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load favorite bookmarks for editor failed: %w", err)
	}
	bookmarksPath, err := configPath("bookmarks")
	if err != nil {
		return nil, nil, err
	}
	bookmarksRaw, err = readFile(bookmarksPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load normal bookmarks for editor failed: %w", err)
	}
	return appsRaw, bookmarksRaw, nil
}

func editorBookmarkRevision(appsRaw, bookmarksRaw []byte) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "%d:", len(appsRaw))
	hash.Write(appsRaw)
	hash.Write(bookmarksRaw)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func editorBookmarksJSON(appsRaw, bookmarksRaw []byte) (categories string, bookmarks string, err error) {
	favoriteBookmarks, err := loadBookmarksFromRaw(appsRaw, true)
	if err != nil {
		return "", "", fmt.Errorf("load favorite bookmarks for editor failed: %w", err)
	}
	normalBookmarks, err := loadBookmarksFromRaw(bookmarksRaw, false)
	if err != nil {
		return "", "", fmt.Errorf("load normal bookmarks for editor failed: %w", err)
	}
	for index := range normalBookmarks.Categories {
		normalBookmarks.Categories[index].ID = strings.TrimSpace(normalBookmarks.Categories[index].ID)
		normalBookmarks.Categories[index].Name = strings.TrimSpace(normalBookmarks.Categories[index].Name)
	}
	for index := range normalBookmarks.Items {
		normalBookmarks.Items[index].Category = strings.TrimSpace(normalBookmarks.Items[index].Category)
	}

	mixedBookmarks := make([]editorBookmark, 0, len(favoriteBookmarks.Items)+len(normalBookmarks.Items))
	for _, item := range favoriteBookmarks.Items {
		mixedBookmarks = append(mixedBookmarks, editorBookmark{Bookmark: item, IsApplication: true})
	}
	for _, item := range normalBookmarks.Items {
		mixedBookmarks = append(mixedBookmarks, editorBookmark{Bookmark: item})
	}

	categories, err = jsonStringify(normalBookmarks.Categories)
	if err != nil {
		return "", "", fmt.Errorf("marshal editor categories failed: %w", err)
	}
	bookmarks, err = jsonStringify(mixedBookmarks)
	if err != nil {
		return "", "", fmt.Errorf("marshal editor bookmarks failed: %w", err)
	}
	return categories, bookmarks, nil
}

func getCategoriesFromCSV(input string) (result []model.Category, err error) {
	reader := csv.NewReader(strings.NewReader(input))
	reader.FieldsPerRecord = -1

	validItems := make([]model.Category, 0)
	seenIDs := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	row := 0

	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, readErr
		}
		row++
		record = normalizeCSVRecord(record)
		if isBlankCSVRecord(record) {
			continue
		}
		if len(record) > 2 {
			return result, fmt.Errorf("category row %d has too many fields", row)
		}

		item := model.Category{
			ID:   csvRecordValue(record, 0),
			Name: csvRecordValue(record, 1),
		}
		if item.ID == "" || item.Name == "" {
			return result, fmt.Errorf("category row %d is incomplete: both ID and Name are required", row)
		}
		if item.Name == editorFixedCategory || item.Name == editorUngroupedCategory {
			return result, fmt.Errorf("\u5206\u7c7b\u540d\u79f0 %q \u4e3a\u4fdd\u7559\u540d\u79f0\uff0c\u8bf7\u4f7f\u7528\u5176\u4ed6\u540d\u79f0", item.Name)
		}
		if _, exists := seenIDs[item.ID]; exists {
			return result, fmt.Errorf("\u5206\u7c7b ID %q \u91cd\u590d\uff0c\u8bf7\u4fdd\u6301\u552f\u4e00", item.ID)
		}
		if _, exists := seenNames[item.Name]; exists {
			return result, fmt.Errorf("\u5206\u7c7b\u540d\u79f0 %q \u91cd\u590d\uff0c\u8bf7\u4fdd\u6301\u552f\u4e00", item.Name)
		}

		seenIDs[item.ID] = struct{}{}
		seenNames[item.Name] = struct{}{}
		validItems = append(validItems, item)
	}

	return validItems, nil
}
func getBookmarksFromCSV(input string, categories []model.Category) (favoriteBookmarks []model.Bookmark, normalBookmarks []model.Bookmark, err error) {
	reader := csv.NewReader(strings.NewReader(input))
	reader.FieldsPerRecord = -1
	row := 0

	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return favoriteBookmarks, normalBookmarks, readErr
		}
		row++
		record = normalizeCSVRecord(record)
		if isBlankCSVRecord(record) {
			continue
		}

		bookmark, parseErr := parseBookmarkCSVRecord(record, row)
		if parseErr != nil {
			return favoriteBookmarks, normalBookmarks, parseErr
		}
		if bookmark.Name == "" && bookmark.URL == "" {
			continue
		}
		if bookmark.Name == "" || bookmark.URL == "" {
			return favoriteBookmarks, normalBookmarks, fmt.Errorf("bookmark row %d is incomplete: both Name and URL are required", row)
		}

		if bookmark.Category == editorUngroupedCategory {
			bookmark.Category = ""
			normalBookmarks = append(normalBookmarks, bookmark)
			continue
		}
		if bookmark.Category == editorFixedCategory || bookmark.Category == "" {
			bookmark.Category = ""
			bookmark.Favorite = false
			favoriteBookmarks = append(favoriteBookmarks, bookmark)
			continue
		}

		foundCategory := false
		for _, category := range categories {
			if category.Name == bookmark.Category {
				bookmark.Category = category.ID
				foundCategory = true
				break
			}
		}
		if !foundCategory {
			return favoriteBookmarks, normalBookmarks, fmt.Errorf("\u4e66\u7b7e %q \u5f15\u7528\u4e86\u4e0d\u5b58\u5728\u7684\u5206\u7c7b %q", bookmark.Name, bookmark.Category)
		}
		normalBookmarks = append(normalBookmarks, bookmark)
	}

	return favoriteBookmarks, normalBookmarks, nil
}

func normalizeCSVRecord(record []string) []string {
	normalized := make([]string, len(record))
	for i, field := range record {
		normalized[i] = strings.TrimSpace(field)
	}
	return normalized
}

func csvRecordValue(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return record[index]
}

func isBlankCSVRecord(record []string) bool {
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

func parseEditorBool(value string, row int, field string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("bookmark row %d has invalid %s value %q", row, field, value)
	}
}

func parseBookmarkCSVRecord(record []string, row int) (model.Bookmark, error) {
	bookmark := model.Bookmark{}
	var err error
	switch len(record) {
	case 6:
		bookmark.Name = csvRecordValue(record, 1)
		bookmark.URL = csvRecordValue(record, 2)
		bookmark.Category = csvRecordValue(record, 3)
		bookmark.Icon = csvRecordValue(record, 4)
		bookmark.Desc = csvRecordValue(record, 5)
	case 7:
		bookmark.Name = csvRecordValue(record, 1)
		bookmark.URL = csvRecordValue(record, 2)
		bookmark.Category = csvRecordValue(record, 3)
		bookmark.Subdir = csvRecordValue(record, 4)
		bookmark.Icon = csvRecordValue(record, 5)
		bookmark.Desc = csvRecordValue(record, 6)
	case 8:
		bookmark.Name = csvRecordValue(record, 1)
		bookmark.URL = csvRecordValue(record, 2)
		bookmark.LocalURL = csvRecordValue(record, 3)
		bookmark.Category = csvRecordValue(record, 4)
		bookmark.Subdir = csvRecordValue(record, 5)
		bookmark.Icon = csvRecordValue(record, 6)
		bookmark.Desc = csvRecordValue(record, 7)
	case 10:
		bookmark.Name = csvRecordValue(record, 1)
		bookmark.URL = csvRecordValue(record, 2)
		bookmark.LocalURL = csvRecordValue(record, 3)
		bookmark.Category = csvRecordValue(record, 4)
		bookmark.Subdir = csvRecordValue(record, 5)
		bookmark.Icon = csvRecordValue(record, 6)
		bookmark.Desc = csvRecordValue(record, 7)
		bookmark.Private, err = parseEditorBool(csvRecordValue(record, 8), row, "Private")
		if err != nil {
			return bookmark, err
		}
		bookmark.Favorite, err = parseEditorBool(csvRecordValue(record, 9), row, "Favorite")
		if err != nil {
			return bookmark, err
		}
	default:
		return bookmark, fmt.Errorf("bookmark row %d has unsupported field count: %d", row, len(record))
	}
	return bookmark, nil
}
func UpdateBookmarksFromEditor(categoriesCSV string, bookmarksCSV string) error {
	_, err := updateBookmarksFromEditor(categoriesCSV, bookmarksCSV, nil)
	return err
}

func UpdateBookmarksFromEditorWithRevision(categoriesCSV, bookmarksCSV, revision string) (string, error) {
	return updateBookmarksFromEditor(categoriesCSV, bookmarksCSV, &revision)
}

func updateBookmarksFromEditor(categoriesCSV, bookmarksCSV string, expectedRevision *string) (revision string, err error) {
	err = withConfigWriteLock(func() error {
		categories, err := getCategoriesFromCSV(categoriesCSV)
		if err != nil {
			log.Println("editor categories CSV parse error:", err)
			return fmt.Errorf("\u89e3\u6790\u5206\u7c7b\u6570\u636e\u5931\u8d25: %w", err)
		}

		favorite, normal, err := getBookmarksFromCSV(bookmarksCSV, categories)
		if err != nil {
			log.Println("editor bookmarks CSV parse error:", err)
			return fmt.Errorf("\u89e3\u6790\u4e66\u7b7e\u6570\u636e\u5931\u8d25: %w", err)
		}

		if expectedRevision != nil {
			if *expectedRevision == "" {
				return ErrEditorRevisionConflict
			}
			appsRaw, bookmarksRaw, err := readEditorBookmarkFiles()
			if err != nil {
				return err
			}
			if *expectedRevision != editorBookmarkRevision(appsRaw, bookmarksRaw) {
				return ErrEditorRevisionConflict
			}
		}

		var normalBookmarks model.Bookmarks
		normalBookmarks.Items = normal
		normalBookmarks.Categories = categories

		var favoriteBookmarks model.Bookmarks
		favoriteBookmarks.Items = favorite

		revision, err = saveEditorBookmarkFilesAtomically(map[string]model.Bookmarks{
			"apps":      favoriteBookmarks,
			"bookmarks": normalBookmarks,
		})
		if err != nil {
			return fmt.Errorf("\u4fdd\u5b58\u7f16\u8f91\u6570\u636e\u5931\u8d25: %w", err)
		}

		return nil
	})
	return revision, err
}

type pendingEditorDataFile struct {
	target string
	temp   string
	backup string
}

func saveEditorBookmarkFilesAtomically(files map[string]model.Bookmarks) (string, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	pending := make([]pendingEditorDataFile, 0, len(names))
	rawFiles := make(map[string][]byte, len(names))
	for _, name := range names {
		raw, err := yaml.Marshal(files[name])
		if err != nil {
			cleanupPendingEditorDataFiles(pending)
			return "", err
		}
		if err := ValidateConfigFileSize(name, raw); err != nil {
			cleanupPendingEditorDataFiles(pending)
			return "", err
		}
		item, err := stagePendingEditorDataFile(name, raw)
		if err != nil {
			cleanupPendingEditorDataFiles(pending)
			return "", err
		}
		pending = append(pending, item)
		rawFiles[name] = raw
	}

	for index := range pending {
		item := &pending[index]
		var err error
		item.backup, err = BackupConfigFile(item.target)
		if err != nil {
			return "", errors.Join(err, rollbackPendingEditorDataFiles(pending, index-1))
		}

		if err := editorDataRenamePath(item.temp, item.target); err != nil {
			rollbackErr := rollbackPendingEditorDataFiles(pending, index-1)
			if rollbackErr != nil {
				return "", errors.Join(err, rollbackErr)
			}
			return "", err
		}

		invalidateFileCachePath(item.target)
	}

	for _, item := range pending {
		if item.backup != "" {
			_ = os.Remove(item.backup)
		}
	}
	return editorBookmarkRevision(rawFiles["apps"], rawFiles["bookmarks"]), nil
}

func stagePendingEditorDataFile(name string, raw []byte) (pendingEditorDataFile, error) {
	target, err := configPath(name)
	if err != nil {
		return pendingEditorDataFile{}, err
	}
	mode, err := ConfigFileMode(target)
	if err != nil {
		return pendingEditorDataFile{}, err
	}
	dir := filepath.Dir(target)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return pendingEditorDataFile{}, err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}

	if _, err := temp.Write(raw); err != nil {
		cleanup()
		return pendingEditorDataFile{}, err
	}
	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return pendingEditorDataFile{}, err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return pendingEditorDataFile{}, err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return pendingEditorDataFile{}, err
	}

	return pendingEditorDataFile{
		target: target,
		temp:   tempPath,
	}, nil
}

func cleanupPendingEditorDataFiles(items []pendingEditorDataFile) {
	for _, item := range items {
		if item.temp != "" {
			_ = os.Remove(item.temp)
		}
	}
}

func rollbackPendingEditorDataFiles(items []pendingEditorDataFile, appliedIndex int) error {
	var rollbackErr error
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if item.temp != "" {
			if err := os.Remove(item.temp); err != nil && !os.IsNotExist(err) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		}
		if index > appliedIndex {
			if item.backup != "" {
				if err := os.Remove(item.backup); err != nil && !os.IsNotExist(err) {
					rollbackErr = errors.Join(rollbackErr, err)
				}
			}
			continue
		}

		if item.backup != "" {
			if err := editorDataRenamePath(item.backup, item.target); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		} else if err := os.Remove(item.target); err != nil && !os.IsNotExist(err) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		invalidateFileCachePath(item.target)
	}
	return rollbackErr
}
