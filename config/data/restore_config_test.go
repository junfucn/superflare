package data

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestLegacyRestoreFillsMissingFieldsWithoutDroppingUnknownSettings(t *testing.T) {
	for _, missing := range []string{"", "LoginUser: ''\nLoginPass: ''\nLocalNetworkID: ''\n"} {
		raw := []byte("Title: Legacy\nTheme: blackboard\nFutureSetting:\n  nested: kept\n" + missing + "...\n")
		raw, err := EnsureLoginConfigInRawConfig(raw, "owner", "strong-password")
		require.NoError(t, err)
		raw, err = EnsureLocalNetworkIdentityInRawConfig(raw, "existing-instance")
		require.NoError(t, err)
		options, err := LoadAppConfigFromRaw(raw)
		require.NoError(t, err)
		require.Equal(t, "owner", options.LoginUser)
		require.Equal(t, "strong-password", options.LoginPass)
		require.Equal(t, "existing-instance", options.LocalNetworkID)
		var fields map[string]any
		require.NoError(t, yaml.Unmarshal(raw, &fields))
		require.Contains(t, fields, "FutureSetting")
	}
}

func TestRestorePreservesExplicitAccountAndIdentity(t *testing.T) {
	raw := []byte("Theme: blackboard\nLoginUser: restored\nLoginPass: restored-password\nLocalNetworkID: restored-instance\n")
	next, err := EnsureLoginConfigInRawConfig(raw, "owner", "strong-password")
	require.NoError(t, err)
	next, err = EnsureLocalNetworkIdentityInRawConfig(next, "existing-instance")
	require.NoError(t, err)
	require.Equal(t, raw, next)
}

func TestRestorePreservesScalarTextWhenAddingIdentity(t *testing.T) {
	for _, value := range []string{"on", "NO", "true", "012345", "0xabc", "1e3", "2026-11-17", "!!str 012345"} {
		t.Run(value, func(t *testing.T) {
			raw := []byte("Theme: blackboard\nLoginUser: " + value + "\nLoginPass: " + value + "\nTitle: " + value + "\n")
			before, err := LoadAppConfigFromRaw(raw)
			require.NoError(t, err)
			next, err := EnsureLocalNetworkIdentityInRawConfig(raw, "existing-instance")
			require.NoError(t, err)
			after, err := LoadAppConfigFromRaw(next)
			require.NoError(t, err)
			before.LocalNetworkID = "existing-instance"
			require.Equal(t, before, after)
		})
	}
}

func TestRestorePreservesYAMLValuesAndAliasesWhenAddingCredentials(t *testing.T) {
	raw := []byte(`Theme: blackboard
Title: 012345
ShowApps: on
LocalNetworkID: 0xabc
FutureSetting: &future
  012345: on
  nested: [0xabc, NO, 1e3, '012345']
  text: |
    kept verbatim
FutureCopy: *future
Defaults: &defaults
  ShowTitle: yes
<<: *defaults
...
`)
	before, err := LoadAppConfigFromRaw(raw)
	require.NoError(t, err)
	var fieldsBefore map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &fieldsBefore))
	next, err := EnsureLoginConfigInRawConfig(raw, "on", "012345")
	require.NoError(t, err)
	next, err = EnsureLocalNetworkIdentityInRawConfig(next, "existing-instance")
	require.NoError(t, err)
	after, err := LoadAppConfigFromRaw(next)
	require.NoError(t, err)
	before.LoginUser, before.LoginPass = "on", "012345"
	require.Equal(t, before, after)
	var fieldsAfter map[string]any
	require.NoError(t, yaml.Unmarshal(next, &fieldsAfter))
	delete(fieldsAfter, "LoginUser")
	delete(fieldsAfter, "LoginPass")
	require.Equal(t, fieldsBefore, fieldsAfter)
}

func TestRestoreDoesNotChangeFieldsAliasingEmptySettings(t *testing.T) {
	raw := []byte("Theme: blackboard\nLoginUser: &empty ''\nLoginPass: *empty\nLocalNetworkID: *empty\nTitle: *empty\nFutureSetting: *empty\n")
	next, err := EnsureLoginConfigInRawConfig(raw, "owner", "strong-password")
	require.NoError(t, err)
	next, err = EnsureLocalNetworkIdentityInRawConfig(next, "existing-instance")
	require.NoError(t, err)
	options, err := LoadAppConfigFromRaw(next)
	require.NoError(t, err)
	require.Equal(t, "owner", options.LoginUser)
	require.Equal(t, "strong-password", options.LoginPass)
	require.Equal(t, "existing-instance", options.LocalNetworkID)
	require.Empty(t, options.Title)
	var fields map[string]any
	require.NoError(t, yaml.Unmarshal(next, &fields))
	require.Equal(t, "", fields["FutureSetting"])
}

func TestRestoreRejectsPartialAccountAndMissingFallback(t *testing.T) {
	for _, fields := range []string{"LoginUser: restored\n", "LoginPass: restored-password\n"} {
		_, err := EnsureLoginConfigInRawConfig([]byte("Theme: blackboard\n"+fields), "owner", "strong-password")
		require.ErrorContains(t, err, "credentials are incomplete")
	}
	_, err := EnsureLoginConfigInRawConfig([]byte("Theme: blackboard\n"), "", "")
	require.Error(t, err)
	raw, err := EnsureLocalNetworkIdentityInRawConfig([]byte("Theme: blackboard\n"), "")
	require.NoError(t, err)
	options, err := LoadAppConfigFromRaw(raw)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(strings.TrimSpace(options.LocalNetworkID)), 26)
}
