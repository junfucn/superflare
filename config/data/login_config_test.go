package data

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateLoginConfigUpdatesConfigAndEnvTogether(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: old-user\nLoginPass: old-pass\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FLARE_USER=env-old-user\nFLARE_PASS=env-old-pass\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := UpdateLoginConfig("new-user", "new-pass"); err != nil {
		t.Fatalf("expected UpdateLoginConfig to succeed: %v", err)
	}

	configRaw, err := os.ReadFile(filepath.Join(tmpDir, "config.yml"))
	if err != nil {
		t.Fatalf("read config.yml: %v", err)
	}
	configText := string(configRaw)
	if !strings.Contains(configText, "LoginUser: new-user") || !strings.Contains(configText, "LoginPass: new-pass") {
		t.Fatalf("config.yml was not updated with new login config: %s", configText)
	}

	envRaw, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	envText := string(envRaw)
	if !strings.Contains(envText, "FLARE_USER=new-user") || !strings.Contains(envText, "FLARE_PASS=new-pass") {
		t.Fatalf(".env was not updated with new login config: %s", envText)
	}
}

func TestUpdateLoginConfigCallbackRunsAfterCommitUnderWriteLock(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureAppConfigExists(); err != nil {
		t.Fatalf("EnsureAppConfigExists: %v", err)
	}
	if err := os.WriteFile(".env", []byte("FLARE_USER=admin\nFLARE_PASS=old-pass\n"), 0600); err != nil {
		t.Fatal(err)
	}

	called := false
	err := UpdateLoginConfigWithCallback("new-user", "new-pass", func() {
		called = true
		if configWriteMu.TryLock() {
			configWriteMu.Unlock()
			t.Error("runtime publication ran outside the config write lock")
		}
		user, pass, err := GetLoginConfigLocked()
		if err != nil || user != "new-user" || pass != "new-pass" {
			t.Errorf("runtime publication preceded the durable config update: user=%q pass=%q err=%v", user, pass, err)
		}
		envPass, err := readEnvValueFromFile(".env", "FLARE_PASS")
		if err != nil || envPass != "new-pass" {
			t.Errorf("runtime publication preceded the durable env update: pass=%q err=%v", envPass, err)
		}
	})
	if err != nil {
		t.Fatalf("UpdateLoginConfigWithCallback: %v", err)
	}
	if !called {
		t.Fatal("runtime publication callback was not called")
	}
}

func TestUpdateLoginConfigRejectsIncompleteCredentialsBeforeWriting(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	originalConfig := []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: old-user\nLoginPass: old-pass\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), originalConfig, 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	originalEnv := []byte("FLARE_USER=env-old-user\nFLARE_PASS=env-old-pass\n")
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), originalEnv, 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	err = UpdateLoginConfig("new-user", "")
	if err == nil {
		t.Fatal("expected UpdateLoginConfig to fail")
	}
	if !strings.Contains(err.Error(), "login credentials are incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}

	configRaw, err := os.ReadFile(filepath.Join(tmpDir, "config.yml"))
	if err != nil {
		t.Fatalf("read config.yml: %v", err)
	}
	if string(configRaw) != string(originalConfig) {
		t.Fatalf("config.yml should stay unchanged, got: %s", string(configRaw))
	}

	envRaw, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if string(envRaw) != string(originalEnv) {
		t.Fatalf(".env should stay unchanged, got: %s", string(envRaw))
	}
}

func TestUpdateLoginConfigRollsBackConfigWhenEnvReplaceFails(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: old-user\nLoginPass: old-pass\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FLARE_USER=env-old-user\nFLARE_PASS=env-old-pass\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	originalRename := osRename
	defer func() { osRename = originalRename }()

	osRename = func(oldPath string, newPath string) error {
		if filepath.Base(newPath) == "config.yml" && strings.Contains(filepath.Base(oldPath), ".tmp-") {
			return os.ErrPermission
		}
		return originalRename(oldPath, newPath)
	}

	called := false
	if err := UpdateLoginConfigWithCallback("new-user", "new-pass", func() { called = true }); err == nil {
		t.Fatal("expected UpdateLoginConfig to fail")
	}
	if called {
		t.Fatal("runtime credentials were published after the config save failed")
	}

	configRaw, err := os.ReadFile(filepath.Join(tmpDir, "config.yml"))
	if err != nil {
		t.Fatalf("read config.yml: %v", err)
	}
	configText := string(configRaw)
	if !strings.Contains(configText, "LoginUser: old-user") || !strings.Contains(configText, "LoginPass: old-pass") {
		t.Fatalf("config.yml should be rolled back, got: %s", configText)
	}

	envRaw, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	envText := string(envRaw)
	if !strings.Contains(envText, "FLARE_USER=env-old-user") || !strings.Contains(envText, "FLARE_PASS=env-old-pass") {
		t.Fatalf(".env should remain original, got: %s", envText)
	}
}

func TestGetLoginConfigRepairsConfigWhenCredentialsEmptyEvenIfEnvComplete(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: \"\"\nLoginPass: \"\"\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FLARE_USER=env-user\nFLARE_PASS=env-pass\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	user, pass, err := GetLoginConfig()
	if err != nil {
		t.Fatalf("GetLoginConfig: %v", err)
	}
	if user != "admin" || pass != "admin" {
		t.Fatalf("expected repaired default credentials, got user=%q pass=%q", user, pass)
	}
}

func TestGetLoginConfigRepairsEmptyConfigAndFlexibleEnvAssignments(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: \"\"\nLoginPass: \"\"\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("export FLARE_USER = \"env-user\"\nFLARE_PASS = \"env pass\"\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	user, pass, err := GetLoginConfig()
	if err != nil {
		t.Fatalf("GetLoginConfig: %v", err)
	}
	if user != "admin" || pass != "admin" {
		t.Fatalf("expected repaired default credentials, got user=%q pass=%q", user, pass)
	}
}

func TestGetLoginConfigReturnsErrorWhenConfigBrokenEvenIfEnvComplete(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: [broken"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FLARE_USER=env-user\nFLARE_PASS=env-pass\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	_, _, err = GetLoginConfig()
	if err == nil {
		t.Fatal("expected GetLoginConfig to fail")
	}
	if !strings.Contains(err.Error(), "parse config config failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetLoginConfigRepairsConfigCredentialsWhenIncomplete(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: config-user\nLoginPass: \"\"\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FLARE_USER=env-user\nFLARE_PASS=env-pass\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	user, pass, err := GetLoginConfig()
	if err != nil {
		t.Fatalf("GetLoginConfig: %v", err)
	}
	if user != "admin" || pass != "admin" {
		t.Fatalf("expected repaired default credentials, got user=%q pass=%q", user, pass)
	}
}

func TestGetLoginConfigRepairsEnvCredentialsWhenIncomplete(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: \"\"\nLoginPass: \"\"\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FLARE_USER=env-user\nFLARE_PASS=\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	user, pass, err := GetLoginConfig()
	if err != nil {
		t.Fatalf("GetLoginConfig: %v", err)
	}
	if user != "admin" || pass != "admin" {
		t.Fatalf("expected repaired default credentials, got user=%q pass=%q", user, pass)
	}
}

func TestGetLoginConfigReturnsErrorWhenEnvPathIsDirectoryAndFallbackNeeded(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: \"\"\nLoginPass: \"\"\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, ".env"), 0755); err != nil {
		t.Fatalf("mkdir .env: %v", err)
	}

	_, _, err = GetLoginConfig()
	if err == nil {
		t.Fatal("expected GetLoginConfig to fail when .env is a directory")
	}
	if !strings.Contains(err.Error(), "read login config failed") {
		t.Fatalf("expected wrapped read login config error, got %v", err)
	}
}

func TestReadEnvValueFromContentSupportsFlexibleAssignments(t *testing.T) {
	user, err := readEnvValueFromContent([]byte(" export FLARE_USER = \"admin user\" \n"), "FLARE_USER")
	if err != nil {
		t.Fatalf("readEnvValueFromContent: %v", err)
	}
	if user != "admin user" {
		t.Fatalf("expected flexible env assignment to parse, got %q", user)
	}
}

func TestUpdateLoginConfigUpdatesFlexibleEnvAssignments(t *testing.T) {
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	if err := os.WriteFile(filepath.Join(tmpDir, "config.yml"), []byte("Title: SuperFlare\nLocale: zh\nTheme: blackboard\nLoginUser: old-user\nLoginPass: old-pass\n"), 0644); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("export FLARE_USER = old-user\nFLARE_PASS = \"old pass\"\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := UpdateLoginConfig("new-user", "new pass"); err != nil {
		t.Fatalf("UpdateLoginConfig: %v", err)
	}

	envRaw, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	envText := string(envRaw)
	if !strings.Contains(envText, "export FLARE_USER=new-user") {
		t.Fatalf("expected flexible FLARE_USER line to be updated, got: %s", envText)
	}
	if !strings.Contains(envText, `FLARE_PASS="new pass"`) {
		t.Fatalf("expected spaced FLARE_PASS to be quoted and updated, got: %s", envText)
	}
}

func TestGetLoginConfigReturnsErrorWhenGetwdFails(t *testing.T) {
	originalGetwd := osGetwd
	defer func() { osGetwd = originalGetwd }()

	osGetwd = func() (string, error) {
		return "", errors.New("forced getwd failure")
	}

	_, _, err := GetLoginConfig()
	if err == nil {
		t.Fatal("expected GetLoginConfig to fail")
	}
	if !strings.Contains(err.Error(), "resolve config working directory failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetLoginConfigPreservesValidCredentialsWithOptionalEnv(t *testing.T) {
	for _, env := range []string{
		"FLARE_COOKIE_SECRET=other-settings-only\n",
		"FLARE_USER=env-user\n",
		"FLARE_PASS=env-password\n",
		"FLARE_USER=\nFLARE_PASS=\n",
	} {
		t.Run(env, func(t *testing.T) {
			t.Chdir(t.TempDir())
			config := "Title: SuperFlare\nLocale: en\nTheme: blackboard\nLoginUser: saved-user\nLoginPass: saved-strong-password\n"
			for name, content := range map[string]string{"config.yml": config, ".env": env} {
				if err := os.WriteFile(name, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, read := range []func() (string, string, error){GetLoginConfig, GetLoginConfigLocked} {
				user, pass, err := read()
				if err != nil || user != "saved-user" || pass != "saved-strong-password" {
					t.Fatalf("valid credentials changed: user=%q pass=%q err=%v", user, pass, err)
				}
			}
			for name, expected := range map[string]string{"config.yml": config, ".env": env} {
				raw, err := os.ReadFile(name)
				if err != nil || string(raw) != expected {
					t.Fatalf("%s changed during read: %q, err=%v", name, raw, err)
				}
			}
		})
	}
}

func TestGetLoginConfigPreservesCredentialsSavedBeforeRepair(t *testing.T) {
	for _, envExists := range []bool{false, true} {
		t.Run(map[bool]string{false: "new env", true: "existing env"}[envExists], func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("config.yml", []byte("Title: Test\nLocale: en\nTheme: blackboard\nLoginUser: admin\nLoginPass: ''\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if envExists {
				if err := os.WriteFile(".env", []byte("FLARE_USER=admin\nFLARE_PASS=\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var committedConfig, committedEnv []byte
			user, pass, err := getLoginConfig(func(envPath string, existed bool) (string, string, error) {
				// Commit another writer after the incomplete snapshot, before
				// the repair can acquire the config write lock.
				if !existed {
					if err := os.WriteFile(envPath, []byte("FLARE_COOKIE_SECRET=keep-me\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := UpdateLoginConfig("saved-user", "saved-strong-password"); err != nil {
					t.Fatal(err)
				}
				var err error
				committedConfig, err = os.ReadFile("config.yml")
				if err != nil {
					t.Fatal(err)
				}
				committedEnv, err = os.ReadFile(envPath)
				if err != nil {
					t.Fatal(err)
				}
				return repairPersistentLoginCredentials(envPath, existed)
			})
			if err != nil || user != "saved-user" || pass != "saved-strong-password" {
				t.Fatalf("stale repair returned the wrong credentials: user=%q pass=%q err=%v", user, pass, err)
			}
			for name, want := range map[string][]byte{"config.yml": committedConfig, ".env": committedEnv} {
				raw, err := os.ReadFile(name)
				if err != nil || string(raw) != string(want) {
					t.Fatalf("stale repair changed committed %s: %q, err=%v", name, raw, err)
				}
			}
		})
	}
}

func TestGetLoginConfigRepairRechecksEnvExistence(t *testing.T) {
	for _, initiallyExists := range []bool{false, true} {
		t.Run(map[bool]string{false: "created before repair", true: "removed before repair"}[initiallyExists], func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("config.yml", []byte("Title: Test\nLocale: en\nTheme: blackboard\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if initiallyExists {
				if err := os.WriteFile(".env", []byte("FLARE_USER=\nFLARE_PASS=\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			user, pass, err := getLoginConfig(func(envPath string, existed bool) (string, string, error) {
				var err error
				if existed {
					err = os.Remove(envPath)
				} else {
					err = os.WriteFile(envPath, []byte("FLARE_COOKIE_SECRET=keep-me\n"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				return repairPersistentLoginCredentials(envPath, existed)
			})
			if err != nil || user != "admin" || pass != "admin" {
				t.Fatalf("repair failed after .env changed: user=%q pass=%q err=%v", user, pass, err)
			}
			raw, err := os.ReadFile(".env")
			if initiallyExists {
				if !os.IsNotExist(err) {
					t.Fatalf("repair recreated removed .env: %q, err=%v", raw, err)
				}
			} else if err != nil || !strings.Contains(string(raw), "FLARE_COOKIE_SECRET=keep-me") ||
				!strings.Contains(string(raw), "FLARE_USER=admin") || !strings.Contains(string(raw), "FLARE_PASS=admin") {
				t.Fatalf("repair did not update the new .env correctly: %q, err=%v", raw, err)
			}
		})
	}
}

func TestRestoredCredentialsRejectUnsupportedValues(t *testing.T) {
	for _, value := range []string{
		strings.Repeat("p", LoginPassMaxBytes+1),
		`"password\nnext"`, `"password\u0000next"`,
	} {
		_, err := LoadAppConfigFromRaw([]byte("Title: Test\nLocale: en\nTheme: blackboard\nLoginUser: admin\nLoginPass: " + value + "\n"))
		if err == nil {
			t.Fatal("restore accepted unsupported credentials")
		}
	}
}
