package diffreview

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// LabelForSpec is the overlay title suffix for a /diff argument list.
func LabelForSpec(spec []string) string {
	if len(spec) == 0 {
		return "working tree"
	}
	switch strings.ToLower(spec[0]) {
	case "staged", "--staged", "--cached":
		return "staged"
	case "head":
		return "HEAD"
	default:
		return strings.Join(spec, " ")
	}
}

// GitArgv is the git command that produces a unified diff for spec.
func GitArgv(spec []string) []string {
	argv := []string{"git", "-c", "color.ui=never"}
	if len(spec) == 0 {
		return append(argv, "diff")
	}
	switch strings.ToLower(spec[0]) {
	case "staged", "--staged", "--cached":
		return append(argv, append([]string{"diff", "--staged"}, spec[1:]...)...)
	case "head":
		return append(argv, "show", "--pretty=medium", "-p", "HEAD")
	case "--":
		if len(spec) == 1 {
			return append(argv, "diff")
		}
		return spec[1:]
	default:
		return append(argv, append([]string{"diff"}, spec...)...)
	}
}

// LoadGit runs git in cwd and returns unified-diff text.
func LoadGit(ctx context.Context, cwd string, spec []string) (string, error) {
	argv := GitArgv(spec)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // G204: git from GitArgv, or /diff -- argv
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil && strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	if err != nil && !looksLikeDiff(text) {
		return "", fmt.Errorf("%s: %s", strings.Join(argv, " "), strings.TrimSpace(text))
	}
	return text, nil
}

func looksLikeDiff(text string) bool {
	return strings.Contains(text, "\ndiff --git ") || strings.HasPrefix(text, "diff --git ") ||
		strings.HasPrefix(text, "commit ")
}
