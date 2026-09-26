package shellhist

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppend(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	s := New(dir)
	require.NoError(t, s.Append(Entry{Version: 1, Command: "printf 'hello\\n'"}))
	data, err := os.ReadFile(filepath.Join(dir, "history.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"exit":null`)
	assert.Equal(t, byte('\n'), data[len(data)-1])
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
		info, err = os.Stat(filepath.Join(dir, "history.jsonl"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	require.ErrorContains(t, s.Append(Entry{Command: "ls"}), "version 1")
	require.ErrorContains(t, s.Append(Entry{Version: 1, Command: " \n"}), "blank")
	require.ErrorContains(t, s.Append(Entry{Version: 1, Command: strings.Repeat("x", maxRecord)}), "64 KiB")
}
