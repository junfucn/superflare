package data

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadConfigScalarAllowsStructuredSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	raw := []byte("Theme: blackboard\nLoginUser: owner\nLoginPass: strong-password\nFutureSetting:\n  nested: kept\nOtherSettings: [one, two]\n")
	_, err := LoadAppConfigFromRaw(raw)
	require.NoError(t, err, "the server accepts this configuration")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	for key, want := range map[string]string{"LoginUser": "owner", "LoginPass": "strong-password", "Missing": ""} {
		value, err := ReadConfigScalar("yaml", path, key)
		require.NoError(t, err)
		require.Equal(t, want, value)
	}
}

func TestReadConfigScalarPreservesYAMLValues(t *testing.T) {
	for _, value := range []string{"on", "NO", "true", "012345", "0xabc", "1e3", "2026-03-27"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			require.NoError(t, os.WriteFile(path, []byte("LoginPass: "+value+"\n"), 0600))
			got, err := ReadConfigScalar("yaml", path, "LoginPass")
			require.NoError(t, err)
			require.Equal(t, value, got)
		})
	}
	for _, tc := range []struct{ name, content, want string }{
		{"scalar alias", "Value: &value 012345\nLoginPass: *value\n", "012345"},
		{"merged alias", "Defaults: &defaults {LoginPass: 012345}\n<<: *defaults\n", "012345"},
		{"merge precedence", "Defaults: &defaults {LoginPass: first}\nOther: &other {LoginPass: second}\n<<: [*defaults, *other]\n", "first"},
		{"explicit override", "Defaults: &defaults {LoginPass: inherited}\nLoginPass: explicit\n<<: *defaults\n", "explicit"},
		{"null", "LoginPass: null\n", ""},
		{"quoted null", "LoginPass: 'null'\n", "null"},
		{"empty document", "", ""},
		{"null document", "null\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0600))
			got, err := ReadConfigScalar("yaml", path, "LoginPass")
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestReadConfigScalarRejectsInvalidYAMLValues(t *testing.T) {
	for _, content := range []string{
		"LoginPass: [secret", "LoginPass: [secret]\n", "LoginPass: {nested: secret}\n",
		"Value: &value [secret]\nLoginPass: *value\n", "Defaults: &defaults {<<: *defaults}\n<<: *defaults\n",
		"[secret]\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0600))
		got, err := ReadConfigScalar("yaml", path, "LoginPass")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
		require.Empty(t, got)
	}
	got, err := ReadConfigScalar("yaml", filepath.Join(t.TempDir(), "missing.yml"), "LoginPass")
	require.NoError(t, err)
	require.Empty(t, got)
}
