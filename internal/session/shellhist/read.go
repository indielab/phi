package shellhist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// seamBytes is how much of the cached tail is re-read to confirm the cache still
// describes the file, catching a truncate-and-regrow that kept the inode.
const seamBytes = 64

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
	info, err := os.Stat(s.dir)
	if os.IsNotExist(err) {
		s.current, s.backup = fileCache{}, fileCache{}
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect shell history directory for reading: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("open shell history lock for reading: %s is not a directory", s.dir)
	}
	// Rotation must not split the current and backup loads across generations.
	// Windows rejects O_CREATE on an existing read-only file, even with O_RDONLY.
	path := filepath.Join(s.dir, "history.lock")
	lock, err := os.Open(path)
	if os.IsNotExist(err) {
		// #nosec G703 -- Fixed filename under the caller's canonical sessions directory.
		lock, err = os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
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
	entries, err := s.tailLocked()
	if err != nil {
		return nil, err
	}
	return copyEntries(entries), nil
}

// tailLocked loads the current tail and, while it is still short of maxEntries
// and the budget allows, the backup tail in front of it. The caller holds s.mu
// and the history.lock rotation lock, so rotation cannot move records between
// the two loads.
func (s *Store) tailLocked() ([]Entry, error) {
	entries := s.current.entries
	remaining := tailBudget - len(s.current.data)
	if len(entries) >= maxEntries || remaining <= 0 {
		s.backup = fileCache{}
		return capTail(entries), nil
	}
	if err := s.backup.load(filepath.Join(s.dir, "history.1.jsonl"), remaining); err != nil {
		return nil, err
	}
	// The backup holds the older records, so it goes in front of the current ones.
	combined := make([]Entry, 0, len(s.backup.entries)+len(entries))
	combined = append(combined, s.backup.entries...)
	combined = append(combined, entries...)
	return capTail(combined), nil
}

// capTail keeps the newest maxEntries entries, for a tail that may hold more.
func capTail(entries []Entry) []Entry {
	if len(entries) > maxEntries {
		return entries[len(entries)-maxEntries:]
	}
	return entries
}

// copyEntries detaches entries from the caches: the slice is new and no Exit
// pointer is shared, so a caller cannot mutate cached state through the result.
// An empty tail stays an empty slice rather than becoming nil.
func copyEntries(entries []Entry) []Entry {
	out := make([]Entry, len(entries))
	copy(out, entries)
	for i, e := range out {
		if e.Exit != nil {
			exit := *e.Exit
			out[i].Exit = &exit
		}
	}
	return out
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
		seam := c.data[max(0, len(c.data)-seamBytes):]
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
	return capTail(entries)
}
