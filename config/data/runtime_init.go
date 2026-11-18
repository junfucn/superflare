package data

import (
	"fmt"
	"strings"
)

func EnsureRuntimeDataFiles() error {
	return withConfigWriteLock(func() error {
		if err := ensureBookmarksConfigExists("apps", true); err != nil {
			return err
		}
		if err := ensureBookmarksConfigExists("bookmarks", false); err != nil {
			return err
		}
		return ensurePortsConfigExists()
	})
}

func EnsureAppConfigExists() error {
	// Serialize initialization with other writers, including other processes.
	return withConfigWriteLock(ensureAppConfigExistsLocked)
}

func ensureAppConfigExistsLocked() error {
	return ensureAppConfigExistsWithLoginLocked("", "")
}

// EnsureAppConfigExistsWithLogin seeds a new instance only. Persisted accounts
// remain authoritative on subsequent starts.
func EnsureAppConfigExistsWithLogin(user, pass string) error {
	return withConfigWriteLock(func() error {
		return ensureAppConfigExistsWithLoginLocked(user, pass)
	})
}

func ensureAppConfigExistsWithLoginLocked(user, pass string) error {
	filePath, err := configPath("config")
	if err != nil {
		return err
	}
	exists, err := pathExists(filePath)
	if err != nil {
		return fmt.Errorf("stat config config failed: %w", err)
	}
	if exists {
		return nil
	}
	if user != "" || pass != "" {
		if err := ValidateLoginCredentialPair(user, pass, "startup config"); err != nil {
			return err
		}
		options, _, err := loadDefaultAppConfig()
		if err != nil {
			return err
		}
		options.LoginUser, options.LoginPass = strings.TrimSpace(user), strings.TrimSpace(pass)
		return saveAppConfigToYamlFileLocked("config", options)
	}
	if _, err := initAppConfigLocked(filePath); err != nil {
		return fmt.Errorf("create app config config failed: %w", err)
	}
	return nil
}

func ensureBookmarksConfigExists(name string, isFavorite bool) error {
	filePath, err := configPath(name)
	if err != nil {
		return err
	}
	exists, err := pathExists(filePath)
	if err != nil {
		return fmt.Errorf("stat bookmarks config %s failed: %w", name, err)
	}
	if exists {
		return nil
	}
	if _, err := initBookmarksLocked(filePath, isFavorite); err != nil {
		return fmt.Errorf("create bookmarks config %s failed: %w", name, err)
	}
	return nil
}

func ensurePortsConfigExists() error {
	filePath, err := portsConfigPath()
	if err != nil {
		return err
	}
	exists, err := pathExists(filePath)
	if err != nil {
		return fmt.Errorf("stat ports config failed: %w", err)
	}
	if exists {
		return nil
	}
	_, out, err := loadDefaultPortsConfig()
	if err != nil {
		return err
	}
	if err := saveFileLocked(filePath, out); err != nil {
		return fmt.Errorf("init ports config failed: %w", err)
	}
	return nil
}
