package diffreview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadGitWorkingTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "Operation not permitted") {
				t.Skipf("sandbox blocked git: %s", out)
			}
			require.NoError(t, err, string(out))
		}
	}
	run("git", "init", "--template=")
	run("git", "config", "user.email", "t@t")
	run("git", "config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("old\n"), 0o644))
	run("git", "add", "a.txt")
	run("git", "commit", "-m", "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("new\n"), 0o644))

	text, err := LoadGit(t.Context(), dir, nil)
	require.NoError(t, err)
	assert.Contains(t, text, "diff --git")
	assert.Contains(t, text, "-old")
	assert.Contains(t, text, "+new")

	_, err = Parse(text)
	require.NoError(t, err)
}

func TestLoadGitMissingRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, err := LoadGit(t.Context(), t.TempDir(), nil)
	require.Error(t, err)
}
