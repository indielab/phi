package shellhist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Cache raw tails so growing files need only incremental I/O. A tail never exceeds
// its budget, including incomplete and malformed records.
type fileCache struct {
	info    os.FileInfo
	start   int64
	data    []byte
	entries []Entry
}

// Read returns at most the most recent 5,000 valid entries, oldest first.
// It examines at most 2 MiB across current and backup history. Invalid records
// and records over 64 KiB are skipped; unterminated records wait for a newline.
func (s *Store) Read() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Rotation must not split the current and backup loads across generations.
	// A read-only descriptor also supports histories whose existing lock is read-only.
	// #nosec G703 -- Fixed filename under the caller's canonical sessions directory.
	lock, err := os.OpenFile(filepath.Join(s.dir, "history.lock"), os.O_CREATE|os.O_RDONLY, 0o600)
	if os.IsNotExist(err) {
		s.current, s.backup = fileCache{}, fileCache{}
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open shell history lock for reading: %w", err)
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return nil, fmt.Errorf("lock shell history for reading: %w", err)
	}
	defer unlockFile(lock)
	if err := s.current.load(filepath.Join(s.dir, "history.jsonl"), tailBudget); err != nil {
		return nil, err
	}
	entries := s.current.entries
	remaining := tailBudget - len(s.current.data)
	if len(entries) < maxEntries && remaining > 0 {
		if err := s.backup.load(filepath.Join(s.dir, "history.1.jsonl"), remaining); err != nil {
			return nil, err
		}
		combined := make([]Entry, 0, len(s.backup.entries)+len(entries))
		combined = append(combined, s.backup.entries...)
		entries = append(combined, entries...)
	} else {
		s.backup = fileCache{}
	}
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
	// Exit pointers must not expose mutable cache state.
	result := make([]Entry, len(entries))
	for i, e := range entries {
		result[i] = e
		if e.Exit != nil {
			exit := *e.Exit
			result[i].Exit = &exit
		}
	}
	return result, nil
}

// Commands returns commands in history order, including repeated commands.
func (s *Store) Commands() ([]string, error) {
	entries, err := s.Read()
	if err != nil {
		return nil, err
	}
	commands := make([]string, len(entries))
	for i, e := range entries {
		commands[i] = e.Command
	}
	return commands, nil
}

func (c *fileCache) load(path string, budget int) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		*c = fileCache{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("open shell history %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat shell history %s: %w", path, err)
	}
	start := max(int64(0), info.Size()-int64(budget))
	reusable := c.info != nil && os.SameFile(c.info, info) && info.Size() >= c.info.Size() && start >= c.start
	if reusable && info.Size() == c.info.Size() && !info.ModTime().Equal(c.info.ModTime()) {
		reusable = false
	}
	// A truncate-and-regrow between reads may preserve the inode and grow the file.
	// Check the cached seam before trusting the incremental offset.
	if reusable && len(c.data) > 0 {
		seam := c.data[max(0, len(c.data)-64):]
		actual := make([]byte, len(seam))
		_, err := f.ReadAt(actual, c.info.Size()-int64(len(seam)))
		reusable = err == nil && bytes.Equal(seam, actual)
	}
	if reusable && info.Size() == c.info.Size() && start == c.start {
		return nil
	}
	data := make([]byte, int(info.Size()-start))
	copied := 0
	if reusable && start < c.info.Size() {
		copied = copy(data, c.data[int(start-c.start):])
	}
	if _, err := f.ReadAt(
		data[copied:],
		start+int64(copied),
	); err != nil &&
		(err != io.EOF || len(data[copied:]) != 0) {
		return fmt.Errorf("read shell history %s: %w", path, err)
	}
	entries := parseTail(data, start > 0)
	*c = fileCache{info: info, start: start, data: data, entries: entries}
	return nil
}

func parseTail(data []byte, clipped bool) []Entry {
	entries := make([]Entry, 0)
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			break
		}
		line := data[:end]
		data = data[end+1:]
		if clipped {
			clipped = false
			continue
		}
		if len(line)+1 > maxRecord {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) == nil && validate(e) == nil {
			entries = append(entries, e)
		}
	}
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
	return entries
}
