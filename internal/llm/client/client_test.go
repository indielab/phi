package client

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestIsAnthropicProvider(t *testing.T) {
	cases := []struct {
		cfg  llm.ModelConfig
		want bool
	}{
		{llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: "https://api.anthropic.com"}, true},
		{llm.ModelConfig{Name: "gpt-4o", BaseURL: "https://api.anthropic.com"}, true},
		{llm.ModelConfig{Name: "claude-3-5-sonnet", BaseURL: "https://api.openai.com/v1"}, true},
		{llm.ModelConfig{Name: "gpt-4o", BaseURL: "https://api.openai.com/v1"}, false},
		{llm.ModelConfig{Name: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1"}, false},
	}
	for i, c := range cases {
		require.Equal(t, c.want, isAnthropicProvider(c.cfg), "case %d: isAnthropicProvider(%+v)", i, c.cfg)
	}
}

func TestClientStreamAnthropicEndToEnd(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Api-Key")
		gotVersion = r.Header.Get("Anthropic-Version")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":3}}}`,
			"",
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
			"",
			`data: {"type":"message_delta","usage":{"output_tokens":2}}`,
			"",
			`data: {"type":"message_stop"}`,
			"",
		}, "\n")))
	}))
	defer srv.Close()

	client := NewClient(
		llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: srv.URL, APIKey: "sk-test"},
		nil,
		"be brief",
	)
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	require.Equal(t, "/v1/messages", gotPath)
	require.Equal(t, "sk-test", gotKey)
	require.NotEmpty(t, gotVersion, "expected Anthropic-Version header")
	var text strings.Builder
	var done *llm.StreamEvent
	for _, ev := range events {
		require.Empty(t, ev.Err, "stream error")
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.Equal(t, "hi", text.String())
	require.NotNil(t, done, "unexpected stream result")
	require.Equal(t, "hi", done.Partial.Choices[0].Message.Content)
	require.Equal(t, 5, done.Partial.Usage.TotalTokens)
}

func TestClientStreamOpenAIEndToEnd(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`data: {"choices":[{"delta":{"role":"assistant","content":"he"}}]}`,
			"",
			`data: {"choices":[{"delta":{"content":"llo"}}]}`,
			"",
			`data: {"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")))
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"}, nil, "")
	events := collectEvents(client.Stream(t.Context(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}}))

	require.Equal(t, "/chat/completions", gotPath)
	var text strings.Builder
	var done *llm.StreamEvent
	for _, ev := range events {
		require.Empty(t, ev.Err, "stream error")
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			text.WriteString(ev.Delta.Content)
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.Equal(t, "hello", text.String())
	require.NotNil(t, done, "unexpected stream result")
	require.Equal(t, "hello", done.Partial.Choices[0].Message.Content)
	require.Equal(t, 6, done.Partial.Usage.TotalTokens)
}

func TestClientCompactAnthropic(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"summary here"}]}`))
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "claude-sonnet-4-20250514", BaseURL: srv.URL, APIKey: "sk-test"}, nil, "")
	out, err := client.Compact(t.Context(), "summarize")
	require.NoError(t, err)
	require.Equal(t, "/v1/messages", gotPath)
	require.Equal(t, "summary here", out)
}

func TestClientCompactOpenAI(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"summary here"}}]}`))
	}))
	defer srv.Close()

	client := NewClient(llm.ModelConfig{Name: "gpt-4o", BaseURL: srv.URL, APIKey: "sk-test"}, nil, "")
	out, err := client.Compact(t.Context(), "summarize")
	require.NoError(t, err)
	require.Equal(t, "/chat/completions", gotPath)
	require.Equal(t, "summary here", out)
}

// collectEvents drains an iter.Seq2 into a slice.
func collectEvents(seq func(func(llm.StreamEvent, error) bool)) []llm.StreamEvent {
	var events []llm.StreamEvent
	for ev, err := range seq {
		if err != nil {
			events = append(events, llm.StreamEvent{Type: llm.StreamEventTypeError, Err: err.Error()})
			continue
		}
		events = append(events, ev)
	}
	return events
}
