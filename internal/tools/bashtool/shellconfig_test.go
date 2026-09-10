package bashtool

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/tooldef"
)

func TestIsLegacyWslBashPath(t *testing.T) {
	for _, p := range []string{
		`C:\Windows\System32\bash.exe`,
		`c:\windows\system32\bash.exe`,
		`C:/Windows/Sysnative/bash.exe`,
	} {
		assert.True(t, isLegacyWslBashPath(p), "want WSL shim for %q", p)
	}
	for _, p := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\WSL\bash.exe`,
		`/bin/bash`,
		`C:\Windows\System32\wsl.exe`,
	} {
		assert.False(t, isLegacyWslBashPath(p), "not a WSL shim for %q", p)
	}
}

func TestConfigForShell(t *testing.T) {
	cfg := configForShell(`C:\Program Files\Git\bin\bash.exe`)
	require.False(t, cfg.stdinMode, "git bash should not use stdin mode")
	require.Len(t, cfg.args, 1)
	require.Equal(t, "-c", cfg.args[0])

	cfg = configForShell(`C:\Windows\System32\bash.exe`)
	require.True(t, cfg.stdinMode, "WSL shim must use stdin transport")
	require.Len(t, cfg.args, 1)
	require.Equal(t, "-s", cfg.args[0])
}

func TestResolveShellConfig(t *testing.T) {
	cfg, err := resolveShellConfig()
	require.NoError(t, err)
	require.NotEmpty(t, cfg.shell, "empty shell")
	if runtime.GOOS == "windows" {
		require.False(t, !cfg.stdinMode && (len(cfg.args) != 1 || cfg.args[0] != "-c"), "windows config: %+v", cfg)
	}
}

func TestPrependPathEntry(t *testing.T) {
	sep := string(os.PathListSeparator)
	got := prependPathEntry([]string{"PATH=/usr/bin" + sep + "/bin"}, "/x/bin")
	want := "PATH=/x/bin" + sep + "/usr/bin" + sep + "/bin"
	require.Equal(t, want, got[0])

	// Already present → unchanged.
	existing := "PATH=/usr/bin" + sep + "/x/bin"
	got = prependPathEntry([]string{existing}, "/x/bin")
	require.Len(t, got, 1)
	require.Equal(t, existing, got[0])

	// Windows-style key casing is matched case-insensitively.
	got = prependPathEntry([]string{"Path=C:\\Windows"}, `C:\Phi\bin`)
	require.True(t, strings.HasPrefix(got[0], "Path="))
	require.Contains(t, got[0], `C:\Phi\bin`)

	// No PATH entry → appended.
	got = prependPathEntry([]string{"HOME=/home/x"}, "/x/bin")
	require.Len(t, got, 2)
	require.Equal(t, "PATH=/x/bin", got[1])
}

func TestBuildShellCommand(t *testing.T) {
	cmd, err := buildShellCommand(t.Context(), "echo hi")
	require.NoError(t, err)
	require.NotNil(t, cmd.SysProcAttr, "expected process-group syscall attr")
	require.NotNil(t, cmd.Cancel, "expected tree-kill cancel")
	require.Equal(t, shellWaitDelay, cmd.WaitDelay)
	require.NotEmpty(t, cmd.Env, "expected enriched env")
}

func TestBuildShellCommandUsesContextCwd(t *testing.T) {
	dir := t.TempDir()
	cmd, err := buildShellCommand(tooldef.WithCwd(t.Context(), dir), "echo hi")
	require.NoError(t, err)
	require.Equal(t, dir, cmd.Dir)
}
