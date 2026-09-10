package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseToolStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want ToolStatus
	}{
		{"queued", ToolQueued},
		{"in-progress", ToolInProgress},
		{"done", ToolDone},
		{"error", ToolError},
		{"cancelled", ToolCancelled},
		{"rejected", ToolRejected},
		{"rejected-by-user", ToolRejected},
		{"", ToolInProgress},
		{"unknown", ToolInProgress},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, ParseToolStatus(tt.in), "%q", tt.in)
	}
}

func TestToolStatusStringRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []ToolStatus{
		ToolQueued, ToolInProgress, ToolDone, ToolError, ToolCancelled, ToolRejected,
	} {
		assert.Equal(t, s, ParseToolStatus(s.String()), s.String())
	}
}
