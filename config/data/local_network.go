package data

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/config/validation"
)

// NormalizeLocalNetworkURL accepts one explicit SuperFlare origin, never a
// service URL or credentials. Only the browser will contact this origin.
func NormalizeLocalNetworkURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || !validation.IsValidHTTPURL(raw) ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("local network URL must be a SuperFlare HTTP or HTTPS origin, e.g. http://192.168.0.10:3636")
	}
	return strings.ToLower(u.Scheme) + "://" + u.Host, nil
}

func EnsureLocalNetworkIdentity() error {
	options, err := GetAllSettingsOptions()
	if err != nil || options.LocalNetworkID != "" {
		return err
	}
	return updateLockedAppConfig("config", func(options model.Application) (model.Application, error) {
		if options.LocalNetworkID == "" {
			options.LocalNetworkID = rand.Text()
		}
		return options, nil
	})
}

func EnsureLocalNetworkIdentityInRawConfig(raw []byte, fallbackID string) ([]byte, error) {
	options, err := LoadAppConfigFromRaw(raw)
	if err != nil {
		return nil, err
	}
	if options.LocalNetworkID != "" {
		return raw, nil
	}
	if strings.TrimSpace(fallbackID) == "" {
		fallbackID = rand.Text()
	}
	return upsertRawConfigFields(raw, yaml.MapItem{Key: "LocalNetworkID", Value: fallbackID})
}
