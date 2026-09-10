package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/permission"
	"github.com/pulseaiclub/phi/internal/session"
	"github.com/pulseaiclub/phi/internal/tools"
)

// sseToolCallChunk encodes one SSE data line carrying a full tool-call delta.
func sseToolCallChunk(id, name, args string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{map[string]any{
					"index":    0,
					"id":       id,
					"type":     "function",
					"function": map[string]any{"name": name, "arguments": args},
				}},
			},
		}},
	})
	if err != nil {
		panic(err)
	}
	return "data: " + string(payload) + "\n\n"
}

func sseTextChunk(text string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{
				"role":    "assistant",
				"content": text,
			},
		}},
	})
	if err != nil {
		panic(err)
	}
	return "data: " + string(payload) + "\n\n"
}

// fakeToolSequenceServer returns tool calls for finalAfter tool requests, then
// returns a final text response. A negative finalAfter means tool calls forever.
func fakeToolSequenceServer(finalAfter int) (*httptest.Server, *atomic.Int32) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		request := requests.Add(1)
		if finalAfter < 0 || int(request) <= finalAfter {
			_, _ = fmt.Fprint(w, sseToolCallChunk(fmt.Sprintf("call_%d", request), "count", `{}`))
		} else {
			_, _ = fmt.Fprint(w, sseTextChunk("done"))
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	return server, &requests
}

func countingTool(runs *atomic.Int32) tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        "count",
			Description: "count tool executions",
			Params:      &llm.FunctionParameters{Type: "object"},
		},
		Run: func(context.Context, json.RawMessage) (tools.Result, error) {
			runs.Add(1)
			return tools.Result{Content: "ok"}, nil
		},
	}
}

func newRoundTestEngine(t *testing.T, serverURL string, runs *atomic.Int32, maxRounds int) *Engine {
	t.Helper()
	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: serverURL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{countingTool(runs)}),
		WithMaxRounds(maxRounds),
	)
	require.NoError(t, err)
	return engine
}

func TestLoopMaxRoundsAllowsFinalAnswerAfterLastToolRound(t *testing.T) {
	server, requests := fakeToolSequenceServer(2)
	defer server.Close()

	var runs atomic.Int32
	engine := newRoundTestEngine(t, server.URL, &runs, 2)

	var lastErr error
	var finalText string
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			lastErr = err
			break
		}
		if update, ok := ev.(session.AssistantMessageUpdate); ok && update.Message.State == session.StateComplete {
			finalText = update.Message.FlatText()
		}
	}
	require.NoError(t, lastErr)
	require.Equal(t, int32(2), runs.Load())
	require.Equal(t, int32(3), requests.Load())
	require.Equal(t, "done", finalText)
}

func TestLoopMaxRoundsDoesNotExecuteExtraToolRound(t *testing.T) {
	server, requests := fakeToolSequenceServer(-1)
	defer server.Close()

	var runs atomic.Int32
	engine := newRoundTestEngine(t, server.URL, &runs, 2)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr, "loop should stop when the model requests a third tool round")
	require.ErrorIs(t, lastErr, ErrMaxRounds)
	require.Equal(t, int32(2), runs.Load())
	require.Equal(t, int32(3), requests.Load())

	assistantToolRounds := 0
	for _, msg := range engine.session.BuildContext() {
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			assistantToolRounds++
		}
	}
	require.Equal(t, 2, assistantToolRounds)
}

func TestLoopContinueAskGrantsAnotherBudget(t *testing.T) {
	server, _ := fakeToolSequenceServer(-1)
	defer server.Close()

	var asks atomic.Int32
	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithMaxRounds(1),
		WithContinueAsk(func(context.Context, int) (bool, error) {
			// Approve once so the loop can start a second budget window, then stop.
			return asks.Add(1) == 1, nil
		}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr)
	require.ErrorIs(t, lastErr, ErrMaxRounds)
	require.Equal(t, int32(2), asks.Load(), "should ask once per exhausted budget")
}

func TestLoopContinueAskDeclineReturnsErrMaxRounds(t *testing.T) {
	server, _ := fakeToolSequenceServer(-1)
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithMaxRounds(1),
		WithContinueAsk(func(context.Context, int) (bool, error) {
			return false, nil
		}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.ErrorIs(t, lastErr, ErrMaxRounds)
}

// overflowThenOKServer fails the first streaming chat with a context overflow,
// serves a non-stream compact summary, then streams a final text reply.
func overflowThenOKServer(streamHits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			n := streamHits.Add(1)
			if n == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"prompt is too long: 210000 > 200000 tokens"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, sseTextChunk("recovered"))
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		// Compaction Compact() is non-streaming chat.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "prior work summarized"},
			}},
		})
	}))
}

func TestLoopOverflowCompactsAndRetries(t *testing.T) {
	var streamHits atomic.Int32
	server := overflowThenOKServer(&streamHits)
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	// Seed enough prior usage that force-compact has a summarizable prefix
	// (default keepRecentTokens is 20k).
	require.NoError(t, sess.Append(
		llm.Message{Role: llm.RoleUser, Content: "old1", Usage: llm.Usage{TotalTokens: 12000}},
		llm.Message{Role: llm.RoleAssistant, Content: "old2", Usage: llm.Usage{TotalTokens: 12000}},
		llm.Message{Role: llm.RoleUser, Content: "old3", Usage: llm.Usage{TotalTokens: 5000}},
		llm.Message{Role: llm.RoleAssistant, Content: "old4", Usage: llm.Usage{TotalTokens: 5000}},
	))

	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 200_000},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	var finalText string
	var sawCompact bool
	for ev, err := range engine.Loop(t.Context(), "continue", LoopOpts{}) {
		if err != nil {
			lastErr = err
			break
		}
		switch e := ev.(type) {
		case session.CompactionStarted:
			sawCompact = true
		case session.AssistantMessageUpdate:
			if e.Message.State == session.StateComplete {
				finalText = e.Message.FlatText()
			}
		}
	}
	require.NoError(t, lastErr)
	require.True(t, sawCompact, "expected overflow path to emit CompactionStarted")
	require.Equal(t, int32(2), streamHits.Load(), "overflow then one retry stream")
	require.Equal(t, "recovered", finalText)
}

func TestLoopOverflowFailsClosedAfterOneRetry(t *testing.T) {
	var streamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if stream, _ := body["stream"].(bool); stream {
			streamHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"prompt is too long: 210000 > 200000 tokens"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": "summary"},
			}},
		})
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	require.NoError(t, sess.Append(
		llm.Message{Role: llm.RoleUser, Content: "old1", Usage: llm.Usage{TotalTokens: 12000}},
		llm.Message{Role: llm.RoleAssistant, Content: "old2", Usage: llm.Usage{TotalTokens: 12000}},
		llm.Message{Role: llm.RoleUser, Content: "old3", Usage: llm.Usage{TotalTokens: 5000}},
		llm.Message{Role: llm.RoleAssistant, Content: "old4", Usage: llm.Usage{TotalTokens: 5000}},
	))

	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 200_000},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "continue", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr)
	require.True(t, llm.IsContextOverflow(lastErr))
	require.Equal(t, int32(2), streamHits.Load(), "one recovery attempt then fail")
}

func TestLoopNonOverflowErrorDoesNotCompact(t *testing.T) {
	var streamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		streamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	var sawCompact bool
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			lastErr = err
			break
		}
		if _, ok := ev.(session.CompactionStarted); ok {
			sawCompact = true
		}
	}
	require.Error(t, lastErr)
	require.False(t, llm.IsContextOverflow(lastErr))
	require.False(t, sawCompact)
	require.Equal(t, int32(1), streamHits.Load())
}
