package data

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPortCollectionSettingDefaultsOnAndPersists(t *testing.T) {
	withTempWorkingDir(t)
	raw := []byte("Title: Saved title\nTheme: blackboard\nLocale: en\nLoginUser: owner\nLoginPass: saved-password\n")
	if err := os.WriteFile("config.yml", raw, 0600); err != nil {
		t.Fatal(err)
	}
	bindings := []byte("ports:\n- port: 8080\n  protocol: tcp\n  remark: preserved\n  hidden: true\n")
	if err := os.WriteFile("ports.yaml", bindings, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := GetAllSettingsOptions()
	if err != nil {
		t.Fatal(err)
	}
	if before.DisablePortCollection {
		t.Fatal("configs without a collection setting must default to enabled")
	}

	for _, enabled := range []bool{false, true} {
		if err := UpdatePortCollectionEnabled(enabled); err != nil {
			t.Fatal(err)
		}
		InvalidateConfigCache("config")
		after, err := GetAllSettingsOptions()
		if err != nil {
			t.Fatal(err)
		}
		expected := before
		expected.DisablePortCollection = !enabled
		if !reflect.DeepEqual(after, expected) {
			t.Fatal("collection setting did not persist or changed unrelated settings")
		}
		saved, err := os.ReadFile("config.yml")
		if err != nil {
			t.Fatal(err)
		}
		if !enabled && !strings.Contains(string(saved), "DisablePortCollection: true") {
			t.Fatal("disabled setting was not written to config.yml")
		}
		afterBindings, err := os.ReadFile("ports.yaml")
		if err != nil || string(afterBindings) != string(bindings) {
			t.Fatalf("collection toggle changed port bindings: %s, err=%v", afterBindings, err)
		}
	}
}
