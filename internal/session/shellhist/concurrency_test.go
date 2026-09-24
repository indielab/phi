package shellhist

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessWriter(t *testing.T) {
	dir := os.Getenv("PHI_SHELLHIST_TEST_DIR")
	if dir == "" {
		return
	}
	s := New(dir)
	for i := range 100 {
		require.NoError(t, s.Append(Entry{Version: 1, Command: fmt.Sprintf("%d:%d", os.Getpid(), i)}))
	}
}

func TestConcurrentProcesses(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	const writers = 4
	results := make(chan error, writers)
	for range writers {
		go func() {
			cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestProcessWriter$")
			cmd.Env = append(os.Environ(), "PHI_SHELLHIST_TEST_DIR="+dir)
			output, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("writer: %w: %s", err, output)
			}
			results <- err
		}()
	}
	for range writers {
		require.NoError(t, <-results)
	}
	commands, err := New(dir).Commands()
	require.NoError(t, err)
	require.Len(t, commands, writers*100)
	unique := make(map[string]bool)
	for _, command := range commands {
		unique[command] = true
	}
	assert.Len(t, unique, writers*100)
}

func TestConcurrentStore(t *testing.T) {
	s := New(t.TempDir())
	var wg sync.WaitGroup
	errors := make(chan error, 400)
	for worker := range 4 {
		wg.Go(func() {
			for i := range 50 {
				errors <- s.Append(Entry{Version: 1, Command: fmt.Sprintf("%d:%d", worker, i)})
				_, err := s.Read()
				errors <- err
			}
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	commands, err := s.Commands()
	require.NoError(t, err)
	assert.Len(t, commands, 200)
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	// Large malformed records must not hide valid records near the rotation seam.
	padding := strings.Repeat("x", rotateSize-len(record("old"))-1) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(padding+record("old")), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.1.jsonl"), []byte(record("obsolete")), 0o600))
	s := New(dir)
	commands, err := s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"old"}, commands)
	require.NoError(t, s.Append(Entry{Version: 1, Command: "new"}))
	commands, err = s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"old", "new"}, commands)
	backup, err := os.Stat(filepath.Join(dir, "history.1.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, int64(rotateSize), backup.Size())
	files, err := filepath.Glob(filepath.Join(dir, "history.*.jsonl"))
	require.NoError(t, err)
	assert.Len(t, files, 1)
}

func TestAppendAfterPartial(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(`{"v":1,"cmd":"broken`), 0o600))
	s := New(dir)
	require.NoError(t, s.Append(Entry{Version: 1, Command: "intact"}))
	commands, err := s.Commands()
	require.NoError(t, err)
	assert.Equal(t, []string{"intact"}, commands)
}
