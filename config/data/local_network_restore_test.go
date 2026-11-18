package data

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestLegacyRawConfigGetsLocalNetworkIdentityWithoutLosingFields(t *testing.T) {
	for _, field := range []string{"", "LocalNetworkID: \"\"\n", "LocalNetworkID: null\n"} {
		raw := []byte("Title: Legacy\nTheme: blackboard\nExtraField:\n  enabled: true\n" + field)
		updated, err := EnsureLocalNetworkIdentityInRawConfig(raw, "current-instance")
		require.NoError(t, err)
		options, err := LoadAppConfigFromRaw(updated)
		require.NoError(t, err)
		require.Equal(t, "current-instance", options.LocalNetworkID)
		var fields map[string]interface{}
		require.NoError(t, yaml.Unmarshal(updated, &fields))
		require.Contains(t, fields, "ExtraField")
		require.Equal(t, false, options.ShowApps)
		require.Equal(t, true, options.ShowFavorites)
		again, err := EnsureLocalNetworkIdentityInRawConfig(updated, "other-instance")
		require.NoError(t, err)
		require.Equal(t, updated, again)
	}
	raw, err := EnsureLocalNetworkIdentityInRawConfig([]byte("Theme: blackboard\n"), "")
	require.NoError(t, err)
	options, err := LoadAppConfigFromRaw(raw)
	require.NoError(t, err)
	require.NotEmpty(t, options.LocalNetworkID)
}
