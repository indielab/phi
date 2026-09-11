package compaction

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldCompact(t *testing.T) {
	settings := Settings{
		enabled:          true,
		reverseTokens:    10,
		keepRecentTokens: 20000,
	}

	// threshold = contextWindow - reverseTokens = 90
	require.False(t, ShouldCompact(50, 100, settings), "below threshold")
	require.False(t, ShouldCompact(90, 100, settings), "at threshold")
	require.True(t, ShouldCompact(95, 100, settings), "above threshold")

	disabled := settings
	disabled.enabled = false
	require.False(t, ShouldCompact(95, 100, disabled), "compaction disabled")

	require.False(t, ShouldCompact(95, 0, settings), "contextWindow <= 0")

	// threshold clamping when reverseTokens > contextWindow
	settings2 := settings
	settings2.reverseTokens = 200
	// threshold becomes 0
	require.False(t, ShouldCompact(0, 100, settings2), "threshold clamped, contextTokens==0")
	require.True(t, ShouldCompact(1, 100, settings2), "threshold clamped, contextTokens>0")
}
