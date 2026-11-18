package data

import (
	"testing"

	"github.com/junfuchang/superflare/config/model"
)

func TestLocalNetworkIdentitySurvivesSettingsUpdates(t *testing.T) {
	withTempWorkingDir(t)
	if err := EnsureAppConfigExists(); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLocalNetworkIdentity(); err != nil {
		t.Fatal(err)
	}
	before, err := GetAllSettingsOptions()
	if err != nil || len(before.LocalNetworkID) < 26 {
		t.Fatalf("identity missing: %v", err)
	}
	origin := " http://192.168.0.10:3636/ "
	if err := UpdateAppearanceWithOptionalNetwork(model.Application{Locale: "zh", IconMode: "NONE"}, nil, &origin); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLocalNetworkIdentity(); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAppearance(model.Application{Locale: "en", IconMode: "NONE"}); err != nil {
		t.Fatal(err)
	}
	after, err := GetAllSettingsOptions()
	if err != nil {
		t.Fatal(err)
	}
	if after.LocalNetworkID != before.LocalNetworkID || after.LocalNetworkURL != "http://192.168.0.10:3636" {
		t.Fatal("identity or origin changed during unrelated settings update")
	}
}

func TestNormalizeLocalNetworkURL(t *testing.T) {
	for _, raw := range []string{"", "http://192.168.1.10:3636", "https://nav.home.arpa/", "http://[fd00::1]:3636"} {
		if _, err := NormalizeLocalNetworkURL(raw); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "//nas", "http://user:pass@nas", "https://nas/service", "https://nas?x=1", "https://nas#foo", "https://nas:bad", "https://nas:65536", "https://nas:"} {
		if _, err := NormalizeLocalNetworkURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
