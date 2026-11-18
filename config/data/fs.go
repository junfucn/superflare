package data

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const configFileMode = 0644

// ConfigFileMode caps credential files at owner-only permissions and never
// grants permissions that an existing target did not have.
func ConfigFileMode(filePath string) (os.FileMode, error) {
	mode := os.FileMode(configFileMode)
	switch strings.ToLower(filepath.Base(filePath)) {
	case "config.yml", "config.yaml", ".env":
		mode = 0600
	}
	info, err := osStat(filePath)
	if err == nil {
		return mode & info.Mode().Perm(), nil
	}
	if !os.IsNotExist(err) {
		return 0, err
	}
	return mode, nil
}

var osRename = os.Rename
var osStat = os.Stat
var osGetwd = os.Getwd

func checkExists(path string) bool {
	exists, _ := pathExists(path)
	return exists
}

func pathExists(path string) (bool, error) {
	_, err := osStat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func configRootDir() (string, error) {
	rootDir, err := osGetwd()
	if err != nil {
		return "", fmt.Errorf("resolve config working directory failed: %w", err)
	}
	return rootDir, nil
}

func configPath(config string) (string, error) {
	rootDir, err := configRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, config+".yml"), nil
}

func getConfigPath(config string) string {
	rootDir, err := configRootDir()
	if err != nil {
		return filepath.Join(".", config+".yml")
	}
	return filepath.Join(rootDir, config+".yml")
}

func GetConfigPath(config string) string {
	return getConfigPath(config)
}

func GetConfigPathErr(config string) (string, error) {
	return configPath(config)
}

type pendingFileCommit struct {
	target string
	temp   string
	backup string
}

// BackupConfigFile copies the original without making the live path disappear.
// Callers must hold the config write lock until replacement or rollback ends.
// A missing target has no backup and is removed again if its creation rolls back.
func BackupConfigFile(target string) (string, error) {
	source, err := os.Open(target)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("target path %s is a directory or non-regular file, cannot overwrite config", target)
	}
	mode, err := ConfigFileMode(target)
	if err != nil {
		return "", err
	}
	backup, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".backup-*")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		_ = backup.Close()
		if !complete {
			_ = os.Remove(backup.Name())
		}
	}()
	if _, err := io.Copy(backup, source); err != nil {
		return "", err
	}
	if err := backup.Chmod(mode); err != nil {
		return "", err
	}
	if err := backup.Sync(); err != nil {
		return "", err
	}
	if err := backup.Close(); err != nil {
		return "", err
	}
	complete = true
	return backup.Name(), nil
}

func saveFile(filePath string, data []byte) error {
	return saveFilesAtomically(map[string][]byte{filePath: data})
}

func stagePendingFileCommit(filePath string, data []byte) (pendingFileCommit, error) {
	switch name := strings.ToLower(filepath.Base(filePath)); name {
	case "config.yml", "config.yaml", "apps.yml", "apps.yaml", "bookmarks.yml", "bookmarks.yaml", "ports.yml", "ports.yaml":
		if err := ValidateConfigFileSize(name, data); err != nil {
			return pendingFileCommit{}, err
		}
	}
	mode, err := ConfigFileMode(filePath)
	if err != nil {
		return pendingFileCommit{}, err
	}
	dir := filepath.Dir(filePath)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(filePath)+".tmp-*")
	if err != nil {
		return pendingFileCommit{}, fmt.Errorf("创建临时文件 %s 失败: %w", filePath, err)
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}

	if _, err := temp.Write(data); err != nil {
		cleanup()
		return pendingFileCommit{}, fmt.Errorf("写入临时文件 %s 失败: %w", filePath, err)
	}
	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return pendingFileCommit{}, fmt.Errorf("设置临时文件权限 %s 失败: %w", filePath, err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return pendingFileCommit{}, fmt.Errorf("同步临时文件 %s 失败: %w", filePath, err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return pendingFileCommit{}, fmt.Errorf("关闭临时文件 %s 失败: %w", filePath, err)
	}

	return pendingFileCommit{
		target: filePath,
		temp:   tempPath,
	}, nil
}

func saveFilesAtomically(files map[string][]byte) error {
	if len(files) == 0 {
		return nil
	}
	return withConfigWriteLock(func() error {
		return saveFilesAtomicallyLocked(files)
	})
}

func saveFilesAtomicallyLocked(files map[string][]byte) error {
	type commitEntry struct {
		path string
		data []byte
	}

	entries := make([]commitEntry, 0, len(files))
	for filePath, raw := range files {
		entries = append(entries, commitEntry{
			path: filepath.Clean(filePath),
			data: raw,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})

	pending := make([]pendingFileCommit, 0, len(entries))
	for _, entry := range entries {
		item, err := stagePendingFileCommit(entry.path, entry.data)
		if err != nil {
			cleanupPendingFileCommits(pending)
			return err
		}
		pending = append(pending, item)
	}

	for index := range pending {
		item := &pending[index]
		var err error
		item.backup, err = BackupConfigFile(item.target)
		if err != nil {
			return errors.Join(err, rollbackPendingFileCommits(pending, index-1))
		}

		if err := osRename(item.temp, item.target); err != nil {
			rollbackErr := rollbackPendingFileCommits(pending, index-1)
			if rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
	}

	for _, item := range pending {
		if item.backup != "" {
			_ = os.Remove(item.backup)
		}
	}
	return nil
}

func saveFileLocked(filePath string, data []byte) error {
	return saveFilesAtomicallyLocked(map[string][]byte{filePath: data})
}

func cleanupPendingFileCommits(items []pendingFileCommit) {
	for _, item := range items {
		if item.temp != "" {
			_ = os.Remove(item.temp)
		}
	}
}

func rollbackPendingFileCommits(items []pendingFileCommit, appliedIndex int) error {
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
			if err := osRename(item.backup, item.target); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			}
		} else if err := os.Remove(item.target); err != nil && !os.IsNotExist(err) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		invalidateFileCachePath(item.target)
	}
	return rollbackErr
}

// readFile reads the file and returns (nil, error) on failure. Callers should handle errors.
func readFile(filePath string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", filePath, err)
	}
	return data, nil
}
