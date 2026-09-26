package shellhist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func record(command string) string {
	data, _ := json.Marshal(Entry{Version: 1, Command: command})
	return string(data) + "\n"
}

func TestReadIncremental(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	commands, err := s.Commands()
	require.NoError(t, err)
	assert.Empty(t, commands)
	path := filepath.Join(dir, "history.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(record("one")+`{"v":1,"cmd":"two"`), 0o600))
	commands, err = s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"one"}, commands)
	oldData := &s.current.data[0]
	_, err = s.Read()
	require.NoError(t, err)
	assert.Same(t, oldData, &s.current.data[0])
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(
		"}\ninvalid\n" + record(
			"three",
		) + `{"v":2,"cmd":"future"}` + "\n" + `{"v":1,"cmd":" "}` + "\n" + strings.Repeat(
			"x",
			maxRecord+1,
		) + "\n" + record(
			"four",
		),
	)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	commands, err = s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two", "three", "four"}, commands)
	require.NoError(t, os.WriteFile(path, []byte(record("short")), 0o600))
	commands, err = s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"short"}, commands)
	replacement := filepath.Join(dir, "replacement")
	require.NoError(t, os.WriteFile(replacement, []byte(record("other")), 0o600))
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Rename(replacement, path))
	commands, err = s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"other"}, commands)
	// Same inode, truncate and regrow past the previous size.
	require.NoError(t, os.WriteFile(path, []byte(record("regrown")+record("longer")), 0o600))
	commands, err = s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"regrown", "longer"}, commands)
}

func TestReadBoundedAndBackup(t *testing.T) {
	dir := t.TempDir()
	var data strings.Builder
	for i := range 6000 {
		data.WriteString(record(fmt.Sprint(i)))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.1.jsonl"), []byte(data.String()), 0o600))
	s := New(dir)
	require.NoError(t, s.Append(Entry{Version: 1, Command: "current"}))
	commands, err := s.Commands()
	require.NoError(t, err)
	require.Len(t, commands, maxEntries)
	assert.Equal(t, "1001", commands[0])
	assert.Equal(t, "current", commands[len(commands)-1])
	data.Reset()
	for i := range 3000 {
		data.WriteString(record(fmt.Sprint(i) + strings.Repeat("x", 1000)))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(data.String()), 0o600))
	commands, err = s.Commands()
	require.NoError(t, err)
	assert.Less(t, len(commands), 3000)
	assert.True(t, strings.HasPrefix(commands[len(commands)-1], "2999"))
	assert.LessOrEqual(t, len(s.current.data)+len(s.backup.data), tailBudget)
	assert.Empty(t, s.backup.data)
}

func TestReadCopiesExit(t *testing.T) {
	s := New(t.TempDir())
	exit := 7
	require.NoError(t, s.Append(Entry{Version: 1, Command: "false", Exit: &exit}))
	entries, err := s.Read()
	require.NoError(t, err)
	*entries[0].Exit = 99
	entries[0].Command = "changed"
	again, err := s.Read()
	require.NoError(t, err)
	assert.Equal(t, "false", again[0].Command)
	assert.Equal(t, 7, *again[0].Exit)
}

func TestReadMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	s := New(dir)
	entries, err := s.Read()
	require.NoError(t, err)
	assert.Empty(t, entries)
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err))
}

func TestReadReadOnlyHistory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.lock"), nil, 0o400))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(record("current")), 0o400))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.1.jsonl"), []byte(record("backup")), 0o400))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o700)) })
	commands, err := New(dir).Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"backup", "current"}, commands)
}

func TestReadLockError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(dir, nil, 0o600))
	_, err := New(dir).Read()
	require.ErrorContains(t, err, "shell history lock for reading")
}

func TestReadCrossStoreRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	padding := strings.Repeat("x", rotateSize-len(record("old"))-1) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(padding+record("old")), 0o600))
	reader, writer := New(dir), New(dir)
	commands, err := reader.Commands()
	require.NoError(t, err)
	require.Equal(t, []string{"old"}, commands)

	lock, err := os.OpenFile(filepath.Join(dir, "history.lock"), os.O_RDWR, 0o600)
	require.NoError(t, err)
	defer lock.Close()
	require.NoError(t, lockFile(lock))
	locked := true
	defer func() {
		if locked {
			unlockFile(lock)
		}
	}()

	type result struct {
		commands []string
		err      error
	}
	started := make(chan struct{})
	done := make(chan result, 1)
	go func() {
		close(started)
		commands, err := reader.Commands()
		done <- result{commands, err}
	}()
	<-started
	select {
	case <-done:
		require.FailNow(t, "read returned while another store held the rotation lock")
	case <-time.After(50 * time.Millisecond):
	}
	// Exercise the writer's rotation while holding the same lock Append uses.
	require.NoError(t, writer.appendRecord([]byte(record("new"))))
	unlockFile(lock)
	locked = false
	select {
	case got := <-done:
		require.NoError(t, got.err)
		assert.Equal(t, []string{"old", "new"}, got.commands)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "read did not resume after rotation unlocked")
	}
	commands, err = reader.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"old", "new"}, commands)
}
