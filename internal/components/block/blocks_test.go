package block_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/components"
	"github.com/pulseaiclub/phi/internal/components/block"
	"github.com/pulseaiclub/phi/internal/components/status"
)

func TestBashBlockRendersOutput(t *testing.T) {
	var lines []string
	for range 20 {
		lines = append(lines, "file.go")
	}
	b := &block.BashBlock{
		Command:  "ls",
		Output:   strings.Join(lines, "\n"),
		Status:   status.ToolDone,
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	s := b.Draw(components.DrawContext{Max: components.Size{Width: 60, Height: 40}})
	joined := components.SurfaceText(s)
	require.Contains(t, joined, "$")
	require.Contains(t, joined, "ls")
	require.NotContains(t, joined, "Show more")
	require.NotContains(t, joined, "lines truncated")
	require.Contains(t, joined, "file.go")
}

func TestUserAndAssistant(t *testing.T) {
	u := &block.UserBlock{Text: "hello", Theme: components.DefaultTheme()}
	us := u.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 5}})
	usText := components.SurfaceText(us)
	require.True(t, strings.Contains(usText, "$ hello") || strings.Contains(usText, "hello"),
		"user: %q", usText)
	a := &block.AssistantBlock{Text: "see `xui` and examples/", Theme: components.DefaultTheme()}
	as := a.Draw(components.DrawContext{Max: components.Size{Width: 60, Height: 5}})
	txt := components.SurfaceText(as)
	require.Contains(t, txt, "xui")
	require.Contains(t, txt, "examples")
}

func TestAgentBlockRendersTreeAndMarkdown(t *testing.T) {
	a := &block.AgentBlock{
		Name:   "agent_spawn",
		Detail: "find bug",
		Status: status.ToolDone,
		Children: []block.ChildTool{
			{Name: "read", Detail: "a.go", Status: status.ToolDone},
			{Name: "bash", Detail: "go test", Status: status.ToolError},
		},
		Summary:  "## Findings\n\n- fixed",
		Expanded: true,
		Theme:    components.DefaultTheme(),
	}
	s := a.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 40}})
	txt := components.SurfaceText(s)
	require.Contains(t, txt, "agent_spawn")
	require.Contains(t, txt, "find bug")
	require.Contains(t, txt, "├──")
	require.Contains(t, txt, "╰──")
	require.Contains(t, txt, "read")
	require.Contains(t, txt, "bash")
	require.Contains(t, txt, "Findings")
	require.Contains(t, txt, "fixed")
	require.NotContains(t, txt, `"job_id"`)
	require.NotContains(t, txt, `"summary"`)
}

func TestUserBlockImplementsWidget(_ *testing.T) {
	var _ components.Widget = &block.UserBlock{Text: "x", Theme: components.DefaultTheme()}
}
