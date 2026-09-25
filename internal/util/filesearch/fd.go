package filesearch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/pulseaiclub/phi/internal/project"
)

var (
	fdPathOnce sync.Once
	fdPath     string
	fdPathErr  error
)

// ResolveFD returns the path to the fd binary: ~/.phi/bin/fd first, then PATH.
func ResolveFD() (string, error) {
	fdPathOnce.Do(func() {
		p, err := project.GetDefaultProject().Global().LookBin("fd")
		if err != nil {
			fdPathErr = errors.New("fd is not available: install to ~/.phi/bin or PATH")
			return
		}
		fdPath = p
	})
	return fdPath, fdPathErr
}

// ResetResolveFDForTest clears the cached fd path (tests only).
func ResetResolveFDForTest() {
	fdPathOnce = sync.Once{}
	fdPath = ""
	fdPathErr = nil
}

// ErrTimeout reports that the search did not finish in time. A large tree with
// no match forces fd to walk everything, which can outlast the caller's
// deadline. Callers show a hint rather than a child-process error.
var ErrTimeout = errors.New("file search timed out")

// Search runs fd under cwd and returns relative paths (slash-separated), at
// most limit. truncated reports that fd found at least limit matches and more
// probably exist, so the caller can say the list is partial.
// An empty query lists files without a name filter.
func Search(ctx context.Context, cwd, query string, limit int) (paths []string, truncated bool, err error) {
	if limit <= 0 {
		limit = 20
	}
	bin, err := ResolveFD()
	if err != nil {
		return nil, false, err
	}
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, false, err
		}
	}

	// Ask for one more than needed: if fd returns limit+1, more matches exist
	// and the caller can say so instead of silently cutting the list.
	args := []string{
		"--type", "f",
		"--color", "never",
		"--max-results", strconv.Itoa(limit + 1),
	}
	query = strings.TrimSpace(query)
	if query != "" {
		// Match against the full path; escape regex metacharacters so the
		// query is treated as a literal substring.
		args = append(args, "--full-path", "--", escapeRegex(query))
	} else {
		// A root with no pattern is read as the pattern, so ask for
		// match-all explicitly.
		args = append(args, "--", ".")
	}
	// Pass the root explicitly rather than relying on cmd.Dir. With cmd.Dir
	// fd emits "./"-relative paths and --full-path then rebuilds a full path
	// for every file, which on a large tree costs about 4x (5.3s vs 1.3s on
	// 363k files). An absolute root needs no such work. Results are the same
	// either way; the prefix is stripped below.
	args = append(args, cwd)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// A cancelled context kills the child, which surfaces as "signal:
		// killed" with empty stderr. Report the cause, not the symptom.
		if ctxErr := ctx.Err(); ctxErr != nil {
			if errors.Is(ctxErr, context.DeadlineExceeded) {
				return nil, false, ErrTimeout
			}
			return nil, false, ctxErr
		}
		// fd exits 1 when there are no matches.
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 && stdout.Len() == 0 {
			return nil, false, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, false, fmt.Errorf("fd: %s", msg)
	}

	lines := strings.Split(stdout.String(), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "." {
			continue
		}
		line = relFromRoot(cwd, line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) > limit {
			// The extra match proves more exist; drop it and say so.
			return out[:limit], true, nil
		}
	}
	return out, false, nil
}

// relFromRoot turns one fd output line into a slash-separated path relative to
// root. fd echoes the root it was given, but not always with the separators phi
// passed: under MSYS2/Git Bash on Windows it prints forward slashes while cwd
// (from os.Getwd) keeps backslashes, so a raw prefix compare misses and leaks an
// absolute path into the @ picker. Windows paths also compare case-insensitively.
// A line that is not a child of root is returned unchanged, so a sibling such as
// /repo2 is not mistaken for /repo.
func relFromRoot(root, line string) string {
	line = filepath.ToSlash(line)
	root = filepath.ToSlash(root)

	rest, ok := strings.CutPrefix(line, root)
	if !ok && runtime.GOOS == "windows" && len(line) >= len(root) && strings.EqualFold(line[:len(root)], root) {
		rest, ok = line[len(root):], true
	}
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) {
		return line
	}
	rest = strings.TrimPrefix(rest, "/")
	return strings.TrimPrefix(rest, "./")
}

func escapeRegex(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 2)
	for _, r := range s {
		switch r {
		case '.', '+', '*', '?', '(', ')', '[', ']', '{', '}', '\\', '|', '^', '$':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
