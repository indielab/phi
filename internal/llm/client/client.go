package client

import (
	"context"
	"iter"
	"net/http"
	"strings"

	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/llm/anthropic"
	"github.com/pulseaiclub/phi/internal/llm/gemini"
	"github.com/pulseaiclub/phi/internal/llm/openai"
	"github.com/pulseaiclub/phi/internal/util"
)

// Client talks to the configured LLM endpoint: OpenAI-compatible by default,
// or Anthropic / Gemini when the config matches (see isAnthropicProvider /
// isGeminiProvider).
type Client struct {
	httpClient *http.Client
	cfg        llm.ModelConfig
	tools      []llm.ToolDefinition
	system     string
	anthropic  bool
	gemini     bool
}

// NewClient builds a streaming chat client.
func NewClient(cfg llm.ModelConfig, tools []llm.ToolDefinition, systemPrompt string) *Client {
	return &Client{
		httpClient: util.DefaultHTTPClient(),
		cfg:        cfg,
		tools:      tools,
		system:     systemPrompt,
		anthropic:  isAnthropicProvider(cfg),
		gemini:     isGeminiProvider(cfg),
	}
}

// Stream runs a streaming chat completion over messages (+ optional system prompt / tools).
func (c *Client) Stream(ctx context.Context, messages []llm.Message) iter.Seq2[llm.StreamEvent, error] {
	switch {
	case c.anthropic:
		req := anthropic.BuildRequest(c.cfg, c.system, messages, c.tools)
		return anthropic.Stream(ctx, c.httpClient, c.cfg, &req)
	case c.gemini:
		req := gemini.BuildRequest(c.system, messages, c.tools)
		return gemini.Stream(ctx, c.httpClient, c.cfg, &req)
	default:
		req := openai.BuildRequest(c.cfg, c.system, messages, c.tools)
		return openai.StreamChatCompletion(ctx, c.httpClient, c.cfg.BaseURL, c.cfg.APIKey, req)
	}
}

// Compact sends a single non-streaming chat request and returns the
// assistant text. It satisfies llm.Compactor for session compaction.
func (c *Client) Compact(ctx context.Context, prompt string) (string, error) {
	switch {
	case c.anthropic:
		return anthropic.Compact(ctx, c.httpClient, c.cfg, prompt)
	case c.gemini:
		return gemini.Compact(ctx, c.httpClient, c.cfg, prompt)
	default:
		return openai.Compact(ctx, c.httpClient, c.cfg, prompt)
	}
}

func isAnthropicProvider(cfg llm.ModelConfig) bool {
	base := strings.ToLower(cfg.BaseURL)
	if strings.Contains(base, "anthropic") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(cfg.Name), "claude")
}

func isGeminiProvider(cfg llm.ModelConfig) bool {
	base := strings.ToLower(cfg.BaseURL)
	name := strings.ToLower(cfg.Name)
	if strings.Contains(base, "generativelanguage.googleapis.com") ||
		strings.Contains(base, "aiplatform.googleapis.com") ||
		strings.Contains(base, "cloudcode-pa.googleapis.com") {
		return true
	}
	return strings.HasPrefix(name, "gemini") || strings.HasPrefix(name, "antigravity-")
}
