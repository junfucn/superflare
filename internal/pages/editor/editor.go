package editor

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/soulteary/memfs"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/define"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/auth"
	"github.com/junfuchang/superflare/internal/background"
	"github.com/junfuchang/superflare/internal/fn"
	"github.com/junfuchang/superflare/internal/footer"
	"github.com/junfuchang/superflare/internal/i18n"
	"github.com/junfuchang/superflare/internal/netutil"
	"github.com/junfuchang/superflare/internal/pool"
	"github.com/junfuchang/superflare/internal/statuspage"
)

var MemFs *memfs.FS
var marshalEditorPortsJSON = json.Marshal
var resolveRestoreConfigPath = restoreConfigPathErr
var refreshPagePaletteCache = define.UpdatePagePalettes
var syncRestoreTempFile = func(file *os.File) error { return file.Sync() }
var renameRestoreFile = os.Rename

const _ASSETS_BASE_DIR = "assets/editor"
const _ASSETS_WEB_URI = "/" + _ASSETS_BASE_DIR
const _ASSETS_TABLE_URI = "/assets/table"
const linkCheckWorkerCount = 8
const linkCheckRequestTimeout = 2 * time.Second
const linkCheckOverallTimeout = 4 * time.Second
const linkCheckRetryDelay = 120 * time.Millisecond
const restoreUploadMaxBytes = 64 * 1024 * 1024
const restoreRequestMaxBytes = restoreUploadMaxBytes + (1 << 20)
const restoreZipEntryMaxBytes = data.MaxConfigFileSize
const restoreBackgroundMaxBytes = 32 * 1024 * 1024
const backupBackgroundPrefix = "uploads/background-source"
const restoreZipMaxEntries = 256
const editorNoticeQueryKey = "notice"
const editorNoticeSaveSuccess = "save_success"
const editorNoticeRestoreSuccess = "restore_success"

type editorRuntimeSnapshot struct {
	DebugMode bool
}

type editorRuntimeHolder struct {
	mu  sync.RWMutex
	set bool
	cfg editorRuntimeSnapshot
}

func (h *editorRuntimeHolder) Load() editorRuntimeSnapshot {
	if h == nil {
		return editorRuntimeSnapshot{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.set {
		return editorRuntimeSnapshot{}
	}
	return h.cfg
}

func (h *editorRuntimeHolder) Store(cfg editorRuntimeSnapshot) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.set = true
	h.cfg = cfg
	h.mu.Unlock()
}

var editorRuntimeFlags = &editorRuntimeHolder{}

func editorRuntimeSnapshotFromFlags(flags model.Flags) editorRuntimeSnapshot {
	return editorRuntimeSnapshot{DebugMode: flags.DebugMode}
}

func currentEditorRuntime() editorRuntimeSnapshot {
	editorRuntimeFlags.mu.RLock()
	hasValue := editorRuntimeFlags.set
	cfg := editorRuntimeFlags.cfg
	editorRuntimeFlags.mu.RUnlock()
	if hasValue {
		return cfg
	}
	cfg = editorRuntimeSnapshotFromFlags(define.CurrentAppRuntimeFlags())
	editorRuntimeFlags.Store(cfg)
	return cfg
}

func SetRuntimeFlags(flags model.Flags) {
	editorRuntimeFlags.Store(editorRuntimeSnapshotFromFlags(flags))
}

//go:embed editor-assets
var editorAssets embed.FS

func Init() error {
	MemFs = memfs.New()
	err := MemFs.MkdirAll(_ASSETS_BASE_DIR, 0777)
	if err != nil {
		return err
	}
	return nil
}

func RegisterRouting(e *echo.Echo) {
	RegisterAssetRouting(e)
	e.GET(define.RegularPages.Editor.Path, render, auth.AuthRequired)
	e.POST(define.RegularPages.Editor.Path, updateData, auth.AuthRequired)
	e.GET(define.RegularPages.Editor.Path+"/backup", backupData, auth.AuthRequired)
	e.POST(define.RegularPages.Editor.Path+"/restore", restoreData, auth.AuthRequired)
	e.POST(define.RegularPages.Editor.Path+"/check-links", checkLinks, auth.AuthRequired)
}

func RegisterAssetRouting(e *echo.Echo) {
	var assetFS fs.FS
	if currentEditorRuntime().DebugMode && registerLocalVendorAssets(e) {
		assetFS = os.DirFS("embed/assets/vendor/editor-assets")
		// Local development can run without copying generated assets first.
	} else if introAssets, err := fs.Sub(editorAssets, "editor-assets"); err == nil {
		assetFS = introAssets
		e.StaticFS(_ASSETS_WEB_URI, introAssets)
	}
	if assetFS != nil {
		registerEditorAssetAliases(e, assetFS)
	}
}

func registerLocalVendorAssets(e *echo.Echo) bool {
	const vendorAssets = "embed/assets/vendor/editor-assets"
	if stat, err := os.Stat(vendorAssets); err == nil && stat.IsDir() {
		e.Static(_ASSETS_WEB_URI, vendorAssets)
		return true
	}
	return false
}

func registerEditorAssetAliases(e *echo.Echo, assetFS fs.FS) {
	e.GET(_ASSETS_TABLE_URI+"/runtime.js", serveEditorAsset(assetFS, "regenerator-runtime.js", "text/javascript; charset=utf-8"))
	e.GET(_ASSETS_TABLE_URI+"/grid.css", serveEditorAsset(assetFS, "handsontable.full.min.css", "text/css; charset=utf-8"))
	e.GET(_ASSETS_TABLE_URI+"/grid.js", serveEditorAsset(assetFS, "handsontable.full.min.js", "text/javascript; charset=utf-8"))
	e.GET(_ASSETS_TABLE_URI+"/grid.zh-CN.js", serveEditorAsset(assetFS, "zh-CN.min.js", "text/javascript; charset=utf-8"))
}

func serveEditorAsset(assetFS fs.FS, name string, contentType string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		raw, err := fs.ReadFile(assetFS, name)
		if err != nil {
			return c.NoContent(http.StatusNotFound)
		}
		return c.Blob(http.StatusOK, contentType, raw)
	}
}

func getDebugAssetVersion() string {
	if currentEditorRuntime().DebugMode {
		return "?v=dev"
	}
	return ""
}

func renderEditorErrorPage(c *echo.Context, status int, err error) error {
	message := ""
	if err != nil {
		message = strings.TrimSpace(err.Error())
	}
	if bindErr := statuspage.BindCurrentOptions(c); bindErr != nil {
		detail := "settings config error: " + strings.TrimSpace(bindErr.Error())
		switch {
		case message == "":
			message = detail
		case !strings.Contains(message, detail) && !strings.Contains(message, strings.TrimSpace(bindErr.Error())):
			message = message + "; " + detail
		}
	}
	return statuspage.HTML(c, status, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), status, message))
}

func editorJSONError(message string) map[string]string {
	return map[string]string{"error": message}
}

func editorWantsJSON(c *echo.Context) bool {
	accept := strings.ToLower((*c).Request().Header.Get(echo.HeaderAccept))
	return strings.Contains(accept, echo.MIMEApplicationJSON)
}

func buildEditorNoticeRedirectURL(notice string) string {
	if strings.TrimSpace(notice) == "" {
		return define.RegularPages.Editor.Path
	}
	return define.RegularPages.Editor.Path + "?" + editorNoticeQueryKey + "=" + url.QueryEscape(notice)
}

func resolveEditorOperationNotice(locale string, code string) map[string]string {
	switch strings.TrimSpace(code) {
	case editorNoticeSaveSuccess:
		if locale == "en" {
			return map[string]string{"Type": "success", "Text": "Bookmark data saved successfully."}
		}
		return map[string]string{"Type": "success", "Text": "书签与分类数据保存成功。"}
	case editorNoticeRestoreSuccess:
		if locale == "en" {
			return map[string]string{"Type": "success", "Text": "Backup restore completed successfully."}
		}
		return map[string]string{"Type": "success", "Text": "备份恢复导入成功。"}
	default:
		return nil
	}
}

func updateData(c *echo.Context) error {
	var body struct {
		Categories *string `form:"categories"`
		Bookmarks  *string `form:"bookmarks"`
		Revision   string  `form:"revision"`
	}
	if err := c.Bind(&body); err != nil || body.Categories == nil || body.Bookmarks == nil {
		if editorWantsJSON(c) {
			return c.JSON(http.StatusBadRequest, editorJSONError("missing form data"))
		}
		return renderEditorErrorPage(c, http.StatusBadRequest, fmt.Errorf("missing form data"))
	}
	revision, err := data.UpdateBookmarksFromEditorWithRevision(*body.Categories, *body.Bookmarks, body.Revision)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, data.ErrEditorRevisionConflict) {
			status = http.StatusConflict
			message := "数据已在其他页面或配置文件中修改。请先复制保留当前修改，再刷新编辑器并合并最新数据后保存。"
			if options, loadErr := data.GetAllSettingsOptions(); loadErr == nil && options.Locale == "en" {
				message = "Data changed elsewhere. Copy your changes before reloading the editor, then merge them with the latest data."
			}
			err = errors.New(message)
		}
		if editorWantsJSON(c) {
			return c.JSON(status, editorJSONError(err.Error()))
		}
		return renderEditorErrorPage(c, status, err)
	}
	if editorWantsJSON(c) {
		return c.JSON(http.StatusOK, map[string]string{"notice": editorNoticeSaveSuccess, "revision": revision})
	}
	return c.Redirect(http.StatusFound, buildEditorNoticeRedirectURL(editorNoticeSaveSuccess))
}

func backupData(c *echo.Context) error {
	raw, err := buildBackupArchive()
	if err != nil {
		return renderEditorErrorPage(c, http.StatusInternalServerError, err)
	}
	filename := "superflare-backup-" + time.Now().Format("20060102-150405") + ".zip"
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+filename+`"`)
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.Blob(http.StatusOK, "application/zip", raw)
}

func buildBackupArchive() ([]byte, error) {
	var raw []byte
	err := background.WithUploadedBackgroundLock(func() error {
		return data.WithConfigWriteLock(func() error {
			var err error
			raw, err = buildBackupArchiveLocked()
			return err
		})
	})
	return raw, err
}

func buildBackupArchiveLocked() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	defer zw.Close()
	var configRaw []byte
	for _, name := range []string{"config", "bookmarks", "apps", "ports"} {
		path, pathErr := resolveRestoreConfigPath(name)
		if pathErr != nil {
			return nil, fmt.Errorf("resolve backup source path failed: %s: %w", name, pathErr)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("backup source file is missing: %s", path)
			}
			return nil, fmt.Errorf("read backup source file failed: %s: %w", path, err)
		}
		if len(raw) == 0 {
			return nil, fmt.Errorf("backup source file is empty: %s", path)
		}
		if len(raw) > restoreZipEntryMaxBytes {
			return nil, fmt.Errorf("backup source file exceeds restore limit: %s", path)
		}
		if name == "config" {
			configRaw = raw
		}
		w, err := zw.Create(restoreConfigFileName(name))
		if err != nil {
			return nil, fmt.Errorf("create backup archive entry failed: %w", err)
		}
		if _, err := w.Write(raw); err != nil {
			return nil, fmt.Errorf("write backup archive entry failed: %w", err)
		}
	}
	options, err := data.LoadAppConfigFromRaw(configRaw)
	if err != nil {
		return nil, fmt.Errorf("read backup configuration failed: %w", err)
	}
	if background.UsesUploadedBackground(options) {
		name, raw, err := background.ReadUploadedBackgroundSource()
		if err != nil {
			return nil, fmt.Errorf("read backup background failed: %w", err)
		}
		name = "uploads/" + name
		if !isBackupBackgroundName(name) {
			return nil, fmt.Errorf("unsupported backup background format")
		}
		entry, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(raw); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finalize backup archive failed: %w", err)
	}
	if buf.Len() > restoreUploadMaxBytes {
		return nil, fmt.Errorf("backup exceeds restore upload limit")
	}
	return buf.Bytes(), nil
}

func restoreData(c *echo.Context) error {
	fail := func(status int, err error) error {
		if editorWantsJSON(c) {
			return c.JSON(status, editorJSONError(err.Error()))
		}
		return renderEditorErrorPage(c, status, err)
	}
	req := c.Request()
	if req.ContentLength > restoreRequestMaxBytes {
		return fail(http.StatusRequestEntityTooLarge, fmt.Errorf("backup request too large"))
	}
	if err := netutil.SetUploadDeadlines(c.Response()); err != nil {
		return fail(http.StatusInternalServerError, err)
	}
	req.Body = http.MaxBytesReader(c.Response(), req.Body, restoreRequestMaxBytes)
	defer func() {
		if req.MultipartForm != nil {
			_ = req.MultipartForm.RemoveAll()
		}
	}()
	file, err := c.FormFile("backup")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fail(http.StatusRequestEntityTooLarge, fmt.Errorf("backup request too large"))
		}
		return fail(http.StatusBadRequest, fmt.Errorf("missing backup file"))
	}
	zipped := strings.HasSuffix(strings.ToLower(file.Filename), ".zip")
	fileLimit := int64(restoreZipEntryMaxBytes)
	if zipped {
		fileLimit = restoreUploadMaxBytes
	}
	if file.Size > fileLimit {
		return fail(http.StatusRequestEntityTooLarge, fmt.Errorf("backup file too large (limit %d MiB)", fileLimit>>20))
	}
	src, err := file.Open()
	if err != nil {
		return fail(http.StatusBadRequest, fmt.Errorf("open backup file failed: %w", err))
	}
	defer src.Close()
	raw, err := io.ReadAll(io.LimitReader(src, fileLimit+1))
	if err != nil {
		return fail(http.StatusBadRequest, fmt.Errorf("read backup file failed: %w", err))
	}

	if int64(len(raw)) > fileLimit {
		return fail(http.StatusRequestEntityTooLarge, fmt.Errorf("backup file too large (limit %d MiB)", fileLimit>>20))
	}
	if zipped {
		if err := restoreZip(raw); err != nil {
			return fail(http.StatusBadRequest, err)
		}
	} else {
		name := normalizeRestoreFileName(file.Filename)
		if !isRestoreConfigName(name) {
			return fail(http.StatusBadRequest, fmt.Errorf("only config/bookmarks/apps/ports yml-yaml files or zip backups are supported"))
		}
		if err := validateRestorePayload(name, raw); err != nil {
			return fail(http.StatusBadRequest, err)
		}
		if err := writeRestoreFilesAtomically(map[string][]byte{name: raw}); err != nil {
			return fail(http.StatusInternalServerError, fmt.Errorf("restore backup failed: %w", err))
		}
	}

	if err := refreshRequestLoginRuntime(c); err != nil {
		return fail(http.StatusInternalServerError, fmt.Errorf("refresh restored login config failed: %w", err))
	}
	if editorWantsJSON(c) {
		return c.JSON(http.StatusOK, map[string]string{"notice": editorNoticeRestoreSuccess})
	}
	return c.Redirect(http.StatusFound, buildEditorNoticeRedirectURL(editorNoticeRestoreSuccess))
}
func restoreZip(raw []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return err
	}
	if len(zr.File) > restoreZipMaxEntries {
		return echo.NewHTTPError(http.StatusBadRequest, "backup zip contains too many files")
	}
	restored := map[string][]byte{}
	backgroundName := ""
	var backgroundData []byte
	totalExpanded := int64(0)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		entryName := strings.ToLower(f.Name)
		isBackground := isBackupBackgroundName(entryName)
		name := strings.TrimPrefix(entryName, "/")
		name = strings.TrimSuffix(name, ".yml")
		name = strings.TrimSuffix(name, ".yaml")
		if !isBackground && (strings.Contains(name, "/") || strings.Contains(name, `\`) || !isRestoreConfigName(name)) {
			continue
		}
		if isBackground && backgroundName != "" {
			return echo.NewHTTPError(http.StatusBadRequest, "backup zip contains duplicate backgrounds")
		}
		if _, exists := restored[name]; exists {
			return echo.NewHTTPError(http.StatusBadRequest, "backup zip contains duplicate restorable files")
		}
		entryLimit := int64(restoreZipEntryMaxBytes)
		if isBackground {
			entryLimit = restoreBackgroundMaxBytes
		}
		if f.UncompressedSize64 > uint64(entryLimit) {
			return echo.NewHTTPError(http.StatusBadRequest, "backup zip contains an oversized data file")
		}
		remaining := int64(restoreUploadMaxBytes) - totalExpanded
		if remaining <= 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "backup zip expands beyond allowed size")
		}
		if remaining < entryLimit {
			entryLimit = remaining
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		rawFile, tooLarge, err := readRestoreZipEntry(rc, entryLimit)
		if err != nil {
			return err
		}
		if tooLarge {
			return echo.NewHTTPError(http.StatusBadRequest, "backup zip contains an oversized data file")
		}
		totalExpanded += int64(len(rawFile))
		if isBackground {
			backgroundName, backgroundData = entryName, rawFile
			continue
		}
		if err := validateRestorePayload(name, rawFile); err != nil {
			return err
		}
		restored[name] = rawFile
	}
	if len(restored) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "backup does not contain any restorable data files")
	}
	return background.WithUploadedBackgroundLock(func() error {
		return data.WithConfigWriteLock(func() error {
			if backgroundName == "" {
				return writeRestoreFilesAtomicallyLocked(restored)
			}
			options, err := data.LoadAppConfigFromRaw(restored["config"])
			if err != nil || !background.UsesUploadedBackground(options) {
				return fmt.Errorf("backup background requires an uploaded-background config")
			}
			if _, err := background.PrepareUploadedBackgroundStage(filepath.Base(backgroundName), bytes.NewReader(backgroundData)); err != nil {
				return fmt.Errorf("restore background failed: %w", err)
			}
			defer background.DiscardStagedUploadedBackgrounds()
			activation, err := background.BeginStagedUploadedBackgroundActivation()
			if err != nil {
				return err
			}
			if err := writeRestoreFilesAtomicallyLocked(restored); err != nil {
				return errors.Join(err, activation.Rollback())
			}
			return activation.Commit()
		})
	})
}

func isBackupBackgroundName(name string) bool {
	switch name {
	case backupBackgroundPrefix + ".jpg", backupBackgroundPrefix + ".jpeg", backupBackgroundPrefix + ".png", backupBackgroundPrefix + ".gif", backupBackgroundPrefix + ".webp":
		return true
	default:
		return false
	}
}

func readRestoreZipEntry(r io.ReadCloser, limit int64) ([]byte, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	closeErr := r.Close()
	if err != nil {
		if closeErr != nil {
			return nil, false, errors.Join(err, closeErr)
		}
		return nil, false, err
	}
	if int64(len(raw)) > limit {
		return nil, true, nil
	}
	if closeErr != nil {
		return nil, false, closeErr
	}
	return raw, false, nil
}

func normalizeRestoreFileName(filename string) string {
	name := strings.ToLower(filename)
	name = strings.TrimSuffix(name, ".yml")
	name = strings.TrimSuffix(name, ".yaml")
	return name
}

func isRestoreConfigName(name string) bool {
	return name == "config" || name == "bookmarks" || name == "apps" || name == "ports"
}

func restoreConfigPathErr(name string) (string, error) {
	if name == "ports" {
		return data.GetPortsConfigPathErr()
	}
	return data.GetConfigPathErr(name)
}

type pendingRestoreFile struct {
	target string
	temp   string
	backup string
	name   string
}

func validateRestorePayload(name string, raw []byte) error {
	if err := data.ValidateConfigFileSize(name, raw); err != nil {
		return err
	}
	switch name {
	case "config":
		options, err := data.LoadAppConfigFromRaw(raw)
		if err != nil {
			return fmt.Errorf("parse config restore payload failed: %w", err)
		}
		if strings.TrimSpace(options.LoginUser) != "" || strings.TrimSpace(options.LoginPass) != "" {
			return data.ValidateLoginCredentialPair(options.LoginUser, options.LoginPass, "restored config.yml")
		}
	case "bookmarks":
		if _, err := data.LoadNormalBookmarksFromRaw(raw); err != nil {
			return fmt.Errorf("parse bookmarks restore payload failed: %w", err)
		}
	case "apps":
		if _, err := data.LoadFavoriteBookmarksFromRaw(raw); err != nil {
			return fmt.Errorf("parse apps restore payload failed: %w", err)
		}
	case "ports":
		if _, err := data.LoadPortBindingsFromRaw(raw); err != nil {
			return fmt.Errorf("parse ports restore payload failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported restore payload name: %s", name)
	}
	return nil
}

func writeRestoreFilesAtomically(files map[string][]byte) error {
	return background.WithUploadedBackgroundLock(func() error {
		return data.WithConfigWriteLock(func() error {
			return writeRestoreFilesAtomicallyLocked(files)
		})
	})
}

func writeRestoreFilesAtomicallyLocked(files map[string][]byte) error {
	var configRaw []byte
	if raw, ok := files["config"]; ok {
		options, err := data.LoadAppConfigFromRaw(raw)
		if err != nil {
			return err
		}
		if background.UsesUploadedBackground(options) {
			if err := background.CheckUploadedBackground(); err != nil {
				return fmt.Errorf("backup requires a missing uploaded background; restore a ZIP containing the image or upload it first: %w", err)
			}
		}
		current, _ := data.GetAllSettingsOptions()
		fallbackUser, fallbackPass := current.LoginUser, current.LoginPass
		if strings.TrimSpace(fallbackUser) == "" || strings.TrimSpace(fallbackPass) == "" {
			active := auth.SnapshotLoginRuntimeConfig()
			fallbackUser, fallbackPass = active.User, active.Pass
		}
		configRaw, err = data.EnsureLoginConfigInRawConfig(raw, fallbackUser, fallbackPass)
		if err != nil {
			return fmt.Errorf("restore login config failed: %w", err)
		}
		configRaw, err = data.EnsureLocalNetworkIdentityInRawConfig(configRaw, current.LocalNetworkID)
		if err != nil {
			return fmt.Errorf("initialize restored local network identity failed: %w", err)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	needsConfigRefresh := false
	for _, name := range names {
		if name == "config" {
			needsConfigRefresh = true
			break
		}
	}

	pending := make([]pendingRestoreFile, 0, len(files))
	for _, name := range names {
		raw := files[name]
		if name == "config" {
			raw = configRaw
		}
		if err := data.ValidateConfigFileSize(name, raw); err != nil {
			cleanupPendingRestoreTemps(pending)
			return err
		}
		target, err := resolveRestoreConfigPath(name)
		if err != nil {
			cleanupPendingRestoreTemps(pending)
			return err
		}
		mode, err := data.ConfigFileMode(target)
		if err != nil {
			cleanupPendingRestoreTemps(pending)
			return err
		}
		dir := filepath.Dir(target)
		temp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".restore-*")
		if err != nil {
			cleanupPendingRestoreTemps(pending)
			return err
		}
		if _, err := temp.Write(raw); err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			cleanupPendingRestoreTemps(pending)
			return err
		}
		if err := temp.Chmod(mode); err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			cleanupPendingRestoreTemps(pending)
			return err
		}
		if err := syncRestoreTempFile(temp); err != nil {
			_ = temp.Close()
			_ = os.Remove(temp.Name())
			cleanupPendingRestoreTemps(pending)
			return err
		}
		if err := temp.Close(); err != nil {
			_ = os.Remove(temp.Name())
			cleanupPendingRestoreTemps(pending)
			return err
		}
		pending = append(pending, pendingRestoreFile{
			target: target,
			temp:   temp.Name(),
			name:   name,
		})
	}

	for index := range pending {
		item := &pending[index]
		var err error
		item.backup, err = data.BackupConfigFile(item.target)
		if err != nil {
			return errors.Join(err, rollbackPendingRestoreFiles(pending, index-1))
		}
		if err := renameRestoreFile(item.temp, item.target); err != nil {
			rollbackErr := rollbackPendingRestoreFiles(pending, index-1)
			if rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
		data.InvalidateConfigCache(item.name)
	}
	if needsConfigRefresh {
		if err := refreshPagePaletteCache(); err != nil {
			rollbackErr := rollbackPendingRestoreFiles(pending, len(pending)-1)
			for _, item := range pending {
				data.InvalidateConfigCache(item.name)
			}
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("refresh page palette cache failed: %w", err), rollbackErr)
			}
			return fmt.Errorf("refresh page palette cache failed: %w", err)
		}
		if err := refreshRuntimeLoginConfigLocked(); err != nil {
			rollbackErr := rollbackPendingRestoreFiles(pending, len(pending)-1)
			for _, item := range pending {
				data.InvalidateConfigCache(item.name)
			}
			paletteRollbackErr := refreshPagePaletteCache()
			if paletteRollbackErr != nil {
				paletteRollbackErr = fmt.Errorf("refresh page palette cache after rollback failed: %w", paletteRollbackErr)
			}
			err = errors.Join(err, paletteRollbackErr)
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("refresh runtime login config failed: %w", err), rollbackErr)
			}
			return fmt.Errorf("refresh runtime login config failed: %w", err)
		}
	}
	for _, item := range pending {
		if item.backup != "" {
			_ = os.Remove(item.backup)
		}
	}
	return nil
}

func cleanupPendingRestoreTemps(items []pendingRestoreFile) {
	for _, item := range items {
		if item.temp == "" {
			continue
		}
		_ = os.Remove(item.temp)
	}
}

func refreshRuntimeLoginConfig() error {
	return data.WithConfigWriteLock(refreshRuntimeLoginConfigLocked)
}

func refreshRuntimeLoginConfigLocked() error {
	user, pass, err := data.GetLoginConfigLocked()
	return storeRuntimeLoginConfig(user, pass, err)
}

func storeRuntimeLoginConfig(user string, pass string, err error) error {
	if err != nil {
		return err
	}
	updated := define.SourceAppRuntimeFlags()
	if strings.TrimSpace(user) != "" {
		updated.User = strings.TrimSpace(user)
		updated.UserIsGenerated = false
	}
	if strings.TrimSpace(pass) != "" {
		updated.Pass = strings.TrimSpace(pass)
		updated.PassIsGenerated = false
	}
	auth.StoreLoginRuntimeConfig(auth.SnapshotLoginRuntimeConfigFromFlags(updated))
	return nil
}

func refreshRequestLoginRuntime(c *echo.Context) error {
	if c == nil {
		return nil
	}
	return data.WithConfigWriteLock(func() error {
		next := auth.SnapshotLoginRuntimeConfigForSessionName(auth.SessionNameForRequest(c))
		if next.User == "" || next.Pass == "" {
			return nil
		}
		previous := auth.SnapshotLoginRuntimeConfigForRequest(c)
		auth.StoreLoginRuntimeConfigForRequest(c, next)
		if previous.User != next.User || previous.Pass != next.Pass {
			auth.InvalidateCurrentLoginSession(c)
		}
		return nil
	})
}

func rollbackPendingRestoreFiles(items []pendingRestoreFile, appliedIndex int) error {
	var rollbackErr error
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if err := os.Remove(item.temp); err != nil && !os.IsNotExist(err) {
			rollbackErr = errors.Join(rollbackErr, err)
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
			if err := renameRestoreFile(item.backup, item.target); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		} else if err := os.Remove(item.target); err != nil && !os.IsNotExist(err) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		data.InvalidateConfigCache(item.name)
	}
	return rollbackErr
}

func restoreConfigFileName(name string) string {
	if name == "ports" {
		return "ports.yaml"
	}
	return name + ".yml"
}

type linkCheckItem struct {
	Row int    `json:"row"`
	URL string `json:"url"`
}

type linkCheckResult struct {
	Row    int    `json:"row"`
	URL    string `json:"url"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type parsedLinkCheckPayload struct {
	Items           []linkCheckItem
	ImmediateResult []linkCheckResult
}

const linkCheckUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36"

func checkLinks(c *echo.Context) error {
	var body struct {
		Bookmarks string `json:"bookmarks" form:"bookmarks"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, editorJSONError("missing form data"))
	}
	payload, err := parseLinksForCheck(body.Bookmarks)
	if err != nil {
		return c.JSON(http.StatusBadRequest, editorJSONError("parse links payload failed: "+err.Error()))
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), linkCheckOverallTimeout)
	defer cancel()
	results := append([]linkCheckResult{}, payload.ImmediateResult...)
	results = append(results, runLinkChecks(ctx, payload.Items)...)
	sortLinkCheckResults(results)
	return c.JSON(http.StatusOK, results)
}

func parseLinksForCheck(input string) (parsedLinkCheckPayload, error) {
	if strings.TrimSpace(input) == "" {
		return parsedLinkCheckPayload{}, nil
	}
	reader := csv.NewReader(strings.NewReader(input))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return parsedLinkCheckPayload{}, err
	}
	result := parsedLinkCheckPayload{
		Items:           make([]linkCheckItem, 0, len(records)),
		ImmediateResult: make([]linkCheckResult, 0),
	}
	for lineIndex, record := range records {
		rowIndex := lineIndex + 1
		record = normalizeLinkCheckRecord(record)
		if isBlankLinkCheckRecord(record) {
			continue
		}
		if len(record) < 3 {
			return parsedLinkCheckPayload{}, fmt.Errorf("link check row %d is incomplete", rowIndex)
		}
		switch len(record) {
		case 6, 7, 8, 10:
		default:
			return parsedLinkCheckPayload{}, fmt.Errorf("link check row %d has unsupported field count: %d", rowIndex, len(record))
		}
		row, convErr := strconv.Atoi(record[0])
		if convErr != nil || row <= 0 {
			return parsedLinkCheckPayload{}, fmt.Errorf("link check row %d has invalid row number %q", rowIndex, record[0])
		}
		rawURL := record[2]
		if rawURL == "" {
			continue
		}
		candidate := classifyLinkCheckCandidate(rawURL)
		if candidate.Skip {
			continue
		}
		if candidate.InvalidReason != "" {
			result.ImmediateResult = append(result.ImmediateResult, linkCheckResult{
				Row:    row,
				URL:    rawURL,
				Status: "invalid",
				Reason: candidate.InvalidReason,
			})
			continue
		}
		result.Items = append(result.Items, linkCheckItem{Row: row, URL: rawURL})
	}
	sortLinkCheckResults(result.ImmediateResult)
	return result, nil
}

func normalizeLinkCheckRecord(record []string) []string {
	normalized := make([]string, len(record))
	for i, field := range record {
		normalized[i] = strings.TrimSpace(field)
	}
	return normalized
}

func isBlankLinkCheckRecord(record []string) bool {
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

type linkCheckCandidate struct {
	Skip          bool
	InvalidReason string
}

func classifyLinkCheckCandidate(rawURL string) linkCheckCandidate {
	u, err := url.Parse(rawURL)
	if err != nil {
		return linkCheckCandidate{InvalidReason: "invalid URL format"}
	}
	if u.Scheme == "" {
		return linkCheckCandidate{InvalidReason: "missing URL scheme"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return linkCheckCandidate{Skip: true}
	}
	if u.Hostname() == "" {
		return linkCheckCandidate{InvalidReason: "missing URL host"}
	}
	if err := validateLinkCheckURL(u); err != nil {
		return linkCheckCandidate{Skip: true}
	}
	return linkCheckCandidate{}
}

type linkCheckRunner func(context.Context, *http.Client, linkCheckItem) linkCheckResult

func runLinkChecks(ctx context.Context, items []linkCheckItem) []linkCheckResult {
	results := make([]linkCheckResult, 0)
	resultsMu := sync.Mutex{}
	client := newLinkCheckClient()
	defer client.CloseIdleConnections()
	return runLinkChecksWithChecker(ctx, client, items, linkCheckWorkerCount, &results, &resultsMu, checkOneLink)
}

func runLinkChecksWithChecker(ctx context.Context, client *http.Client, items []linkCheckItem, workerCount int, results *[]linkCheckResult, resultsMu *sync.Mutex, checker linkCheckRunner) []linkCheckResult {
	if len(items) == 0 {
		return nil
	}
	if workerCount <= 0 {
		workerCount = 1
	}
	if workerCount > len(items) {
		workerCount = len(items)
	}

	jobs := make(chan linkCheckItem)
	scheduledRows := make(map[int]struct{}, len(items))
	scheduledMu := sync.Mutex{}
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-jobs:
					if !ok {
						return
					}
					result := checker(ctx, client, item)
					if result.Status != "ok" {
						resultsMu.Lock()
						*results = append(*results, result)
						resultsMu.Unlock()
					}
				}
			}
		}()
	}

sendLoop:
	for _, item := range items {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- item:
			scheduledMu.Lock()
			scheduledRows[item.Row] = struct{}{}
			scheduledMu.Unlock()
		}
	}
	close(jobs)
	wg.Wait()
	appendPendingLinkCheckResults(ctx, items, scheduledRows, results, resultsMu)
	sortLinkCheckResults(*results)
	return *results
}

func appendPendingLinkCheckResults(ctx context.Context, items []linkCheckItem, scheduledRows map[int]struct{}, results *[]linkCheckResult, resultsMu *sync.Mutex) {
	if ctx == nil || ctx.Err() == nil {
		return
	}
	reason := pendingLinkCheckReason(ctx.Err())
	if reason == "" {
		return
	}
	pending := make([]linkCheckResult, 0)
	for _, item := range items {
		if _, ok := scheduledRows[item.Row]; ok {
			continue
		}
		pending = append(pending, linkCheckResult{
			Row:    item.Row,
			URL:    item.URL,
			Status: "unstable",
			Reason: reason,
		})
	}
	if len(pending) == 0 {
		return
	}
	resultsMu.Lock()
	*results = append(*results, pending...)
	resultsMu.Unlock()
}

func pendingLinkCheckReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "check timed out before this URL could be verified"
	}
	if errors.Is(err, context.Canceled) {
		return "check was canceled before this URL could be verified"
	}
	return strings.TrimSpace(err.Error())
}

func sortLinkCheckResults(results []linkCheckResult) {
	sort.Slice(results, func(i int, j int) bool {
		if results[i].Row == results[j].Row {
			return results[i].URL < results[j].URL
		}
		return results[i].Row < results[j].Row
	})
}

func checkOneLink(ctx context.Context, client *http.Client, item linkCheckItem) linkCheckResult {
	for attempt := 0; attempt < 2; attempt++ {
		result := checkOneLinkOnce(ctx, client, item)
		if result.Status != "unstable" || attempt == 1 {
			return result
		}
		if err := waitForRetry(ctx, linkCheckRetryDelay); err != nil {
			return linkCheckResult{Row: item.Row, URL: item.URL, Status: "unstable", Reason: err.Error()}
		}
	}
	return linkCheckResult{Row: item.Row, URL: item.URL, Status: "unstable", Reason: "unknown transient error"}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func checkOneLinkOnce(ctx context.Context, client *http.Client, item linkCheckItem) linkCheckResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return linkCheckResult{Row: item.Row, URL: item.URL, Status: "invalid", Reason: err.Error()}
	}
	req.Header.Set("User-Agent", linkCheckUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	resp, err := client.Do(req)
	if err != nil {
		status, reason := classifyLinkRequestError(err)
		return linkCheckResult{Row: item.Row, URL: item.URL, Status: status, Reason: reason}
	}
	defer resp.Body.Close()
	status := classifyLinkResponseStatus(resp.StatusCode)
	return linkCheckResult{Row: item.Row, URL: item.URL, Status: status, Reason: resp.Status}
}

func classifyLinkRequestError(err error) (string, string) {
	if errors.Is(err, errLinkCheckDestinationNotAllowed) {
		return "restricted", errLinkCheckDestinationNotAllowed.Error()
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "invalid", err.Error()
		}
		return "unstable", err.Error()
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "unstable", err.Error()
		}
		if netErr.Temporary() {
			return "unstable", err.Error()
		}
	}
	return "unstable", err.Error()
}

func classifyLinkResponseStatus(statusCode int) string {
	switch {
	case statusCode >= 200 && statusCode < 400:
		return "ok"
	case statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusProxyAuthRequired ||
		statusCode == http.StatusMethodNotAllowed ||
		statusCode == http.StatusTooManyRequests ||
		statusCode == http.StatusUnavailableForLegalReasons:
		return "restricted"
	case statusCode == http.StatusRequestTimeout ||
		statusCode == http.StatusMisdirectedRequest ||
		statusCode == http.StatusTooEarly ||
		statusCode >= 500:
		return "unstable"
	default:
		return "invalid"
	}
}

func render(c *echo.Context) error {
	options, err := data.GetAllSettingsOptions()
	if err != nil {
		statuspage.BindOptionsLoadError(c, err)
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
	}
	statuspage.BindOptions(c, options)
	options, renderWarnings := statuspage.PrepareSettingsOptionsForRender(options)
	snapshot, err := data.GetEditorSnapshot()
	if err != nil {
		return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
	}
	portsJSON := "[]"
	localLANHost := ""
	if !auth.IsLoginDisabled(c) {
		portsConfig, err := data.LoadPortBindings()
		if err != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
		}
		portsJSON, err = marshalEditorPorts(portsConfig.Items)
		if err != nil {
			return statuspage.HTML(c, http.StatusInternalServerError, statuspage.BuildHTTPErrorPage(statuspage.CurrentLocale(c), http.StatusInternalServerError, err.Error()))
		}
		if portsJSON != "[]" {
			localLANHost = editorPortHost(options.LocalNetworkURL, c.Request())
			if localLANHost == "" {
				portsJSON = "[]"
				renderWarnings = append(renderWarnings, i18n.T(options.Locale, "editor_port_host_missing"))
			}
		}
	}
	m := pool.GetTemplateMap()
	defer pool.PutTemplateMap(m)
	m["PageName"] = "Editor"
	m["SettingPages"] = define.SettingPages
	m["DebugMode"] = currentEditorRuntime().DebugMode
	m["DebugAssetVersion"] = getDebugAssetVersion()
	m["DataCategories"] = template.HTML(snapshot.Categories)
	m["DataBookmarks"] = template.HTML(snapshot.Bookmarks)
	m["DataRevision"] = snapshot.Revision
	m["DataPorts"] = template.HTML(portsJSON)
	m["LocalLANHost"] = localLANHost
	m["ShowEditorPortPicker"] = localLANHost != ""
	m["OptionTitle"] = options.Title
	m["OptionSiteIcon"] = options.SiteIcon
	m["Locale"] = options.Locale
	footer.BindTemplateData(m, options.Footer)
	m["OptionOpenAppNewTab"] = options.OpenAppNewTab
	m["OptionOpenBookmarkNewTab"] = options.OpenBookmarkNewTab
	m["OptionShowTitle"] = options.ShowTitle
	m["OptionShowDateTime"] = options.ShowDateTime
	m["OptionShowApps"] = options.ShowApps
	m["OptionShowBookmarks"] = options.ShowBookmarks
	m["OperationNotice"] = resolveEditorOperationNotice(options.Locale, c.QueryParam(editorNoticeQueryKey))
	m["RenderWarnings"] = renderWarnings
	return c.Render(http.StatusOK, "editor.html", m)
}

func editorPortHost(origin string, request *http.Request) string {
	if origin == "" {
		if request == nil {
			return ""
		}
		authority := netutil.RequestForwardedHost(request)
		if authority == "" {
			authority = request.Host
		}
		if !fn.HostLooksLocalNetwork(authority) {
			return ""
		}
		origin = "http://" + authority
	}
	origin, err := data.NormalizeLocalNetworkURL(origin)
	if err != nil || origin == "" {
		return ""
	}
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if strings.Contains(host, ":") {
		return "[" + strings.ReplaceAll(host, "%", "%25") + "]"
	}
	return host
}

func marshalEditorPorts(items []model.PortBinding) (string, error) {
	type editorPort struct {
		Port     int    `json:"Port"`
		Protocol string `json:"Protocol"`
		Remark   string `json:"Remark"`
	}
	result := make([]editorPort, 0)
	for _, item := range items {
		if item.Port <= 0 || item.Hidden || strings.ToLower(strings.TrimSpace(item.Protocol)) == "udp" || strings.TrimSpace(item.Remark) == "" {
			continue
		}
		result = append(result, editorPort{Port: item.Port, Protocol: item.Protocol, Remark: strings.TrimSpace(item.Remark)})
	}
	raw, err := marshalEditorPortsJSON(result)
	if err != nil {
		return "", fmt.Errorf("marshal editor ports failed: %w", err)
	}
	return string(raw), nil
}
