package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarshalToolParams(t *testing.T) {
	assert.Equal(t, `{}`, string(MarshalToolParams(nil, "{}")))
	assert.Equal(t, `{"type":"object"}`, string(MarshalToolParams(nil, `{"type":"object"}`)))
	raw := MarshalToolParams(&FunctionParameters{Type: "object", Properties: Object{}}, "{}")
	assert.Contains(t, string(raw), `"type":"object"`)
}

func TestAssistantDone(t *testing.T) {
	ev := AssistantDone("hi", "think", []ToolCall{{ID: "1", Function: Function{Name: "read"}}}, Usage{TotalTokens: 3})
	assert.Equal(t, StreamEventTypeDone, ev.Type)
	assert.Equal(t, "hi", ev.Partial.Choices[0].Message.Content)
	assert.Equal(t, "think", ev.Partial.Choices[0].Message.ReasoningContent)
	assert.Equal(t, 3, ev.Partial.Usage.TotalTokens)
	assert.Len(t, ev.Partial.Choices[0].Message.ToolCalls, 1)
}
