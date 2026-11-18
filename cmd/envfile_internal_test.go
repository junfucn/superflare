package cmd

import (
	"os"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavedCredentialsSurviveEnvStartupParsing(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, os.WriteFile(".env", []byte("FLARE_USER=admin\nFLARE_PASS=old\nFLARE_PORT=3636\n"), 0600))
	for _, password := range []string{"'secret123", "%(FLARE_USER)s-secret", "`strong-password-123", "pass`word", `with"quotes`, `"""triple"""`, `back\slash`, `trailing\`, `with 'single' and "double"`, "space #; pass"} {
		t.Run(password, func(t *testing.T) {
			require.NoError(t, data.UpdateLoginConfig("admin", password))
			flags, err := ParseEnvFileE(model.Flags{Port: 3636})
			require.NoError(t, err)
			require.Equal(t, password, flags.Pass)
			user, pass, err := data.GetLoginConfig()
			require.NoError(t, err)
			require.Equal(t, "admin", user)
			require.Equal(t, password, pass)
		})
	}
	before, err := os.ReadFile(".env")
	require.NoError(t, err)
	require.Error(t, data.UpdateLoginConfig("admin", "bad\nFLARE_PORT=1"))
	after, err := os.ReadFile(".env")
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestParseEnvFile_GetwdFailureReturnsError(t *testing.T) {
	originalResolve := resolveDotEnvPath
	defer func() { resolveDotEnvPath = originalResolve }()

	resolveDotEnvPath = func() (string, error) {
		return "", os.ErrPermission
	}

	baseFlags := model.Flags{
		Port: 3636,
		User: "fallback-user",
		Pass: "fallback-pass",
	}

	flags, err := ParseEnvFileE(baseFlags)
	if err == nil {
		t.Fatal("expected ParseEnvFileE to fail")
	}
	assert.Equal(t, baseFlags, flags)
	assert.ErrorContains(t, err, "resolve .env path failed")
}

func TestParseEnvFileWithoutCredentialsPreservesAccount(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, data.UpdateLoginConfig("saved-user", "saved-strong-password"))
	env := []byte("FLARE_COOKIE_SECRET=separate-secret\nFLARE_PORT=3636\n")
	require.NoError(t, os.WriteFile(".env", env, 0600))
	before, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	base := model.Flags{User: "base-user", Pass: "base-pass", UserIsGenerated: true, PassIsGenerated: true}
	flags, err := ParseEnvFileE(base)
	require.NoError(t, err)
	require.Equal(t, base.User, flags.User)
	require.Equal(t, base.Pass, flags.Pass)
	require.True(t, flags.UserIsGenerated)
	require.True(t, flags.PassIsGenerated)
	require.Equal(t, "separate-secret", flags.CookieSecret)
	flags, err = applyAccountConfig(flags)
	require.NoError(t, err)
	require.Equal(t, "saved-user", flags.User)
	require.Equal(t, "saved-strong-password", flags.Pass)
	after, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)
	afterEnv, err := os.ReadFile(".env")
	require.NoError(t, err)
	require.Equal(t, env, afterEnv)
}

func TestParseEnvFileSupportsExportAssignments(t *testing.T) {
	t.Chdir(t.TempDir())
	content := []byte("export FLARE_USER = \"deployment-user\"\r\n" +
		" export FLARE_PASS = \"strong #; password\"\r\n" +
		"export FLARE_PORT=4321\r\n" +
		"export FLARE_EDITOR=false\r\n" +
		"export FLARE_VISIBILITY=PRIVATE\r\n" +
		"export FLARE_COOKIE_SECRET=\"separate secret\"\r\n" +
		"export FLARE_TRUSTED_PROXIES=none\r\n")
	require.NoError(t, os.WriteFile(".env", content, 0600))
	flags, err := ParseEnvFileE(model.Flags{
		User: "fallback-user", Pass: "fallback-pass", Port: 3636,
		UserIsGenerated: true, PassIsGenerated: true, EnableEditor: true,
	})
	require.NoError(t, err)
	require.Equal(t, "deployment-user", flags.User)
	require.Equal(t, "strong #; password", flags.Pass)
	require.False(t, flags.UserIsGenerated)
	require.False(t, flags.PassIsGenerated)
	require.Equal(t, 4321, flags.Port)
	require.False(t, flags.EnableEditor)
	require.Equal(t, "PRIVATE", flags.Visibility)
	require.Equal(t, "separate secret", flags.CookieSecret)
	require.Equal(t, "none", flags.TrustedProxies)
	after, err := os.ReadFile(".env")
	require.NoError(t, err)
	require.Equal(t, content, after)
}
