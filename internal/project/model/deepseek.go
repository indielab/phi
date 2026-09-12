// Package model holds built-in model presets — connection defaults for the
// models providers advertise, so a config.yaml entry can omit base_url,
// context_window, and image_enabled and still get the right values. Presets
// are keyed by the exact model name the API documents; unknown or legacy
// names fall through to the generic OpenAI defaults the config loader applies.
package model

import "github.com/pulseaiclub/phi/internal/llm"

// Lookup returns the built-in connection defaults for a model name. ok is
// false for names without a preset, so callers fall back to the generic
// OpenAI endpoint. The returned config carries no api_key or skill path;
// the caller layers those on top and may override any field.
func Lookup(name string) (llm.ModelConfig, bool) {
	for _, p := range presets {
		if p.Name == name {
			return p, true
		}
	}
	return llm.ModelConfig{}, false
}

// presets is the built-in catalog, keyed by model name. Values mirror each
// provider's public API docs; re-check the linked page when refreshing a
// model — context length, base URL, and capabilities change between versions.
var presets = []llm.ModelConfig{
	// Source: https://api-docs.deepseek.com/zh-cn/quick_start/pricing
	//
	// Both DeepSeek models share the OpenAI-compatible base URL
	// (https://api.deepseek.com; the /v1 alias and the Anthropic-compatible
	// https://api.deepseek.com/anthropic also work), a 1M-token context
	// window, 384K max output, thinking mode on by default, and support for
	// tool calls, JSON output, and prefix completion. The OpenAI client
	// already enables thinking for deepseek-* names (isThinkingModeModel).
	//
	// Pricing (CNY per 1M tokens; off-peak hours are half of peak — the doc
	// defines peak as Beijing weekdays 09:00-12:00 / 14:00-18:00):
	//
	//   deepseek-flash    cache-hit ¥0.04   cache-miss ¥2.00   output ¥8.00
	//   deepseek-v4-pro   cache-hit ¥0.30   cache-miss ¥9.00  output ¥27.00
	{
		Name:          "deepseek-v4-flash", // DeepSeek-V4.1-Flash
		BaseURL:       "https://api.deepseek.com",
		ContextWindow: 1_000_000,
		ImageEnabled:  true, // V4.1-Flash accepts image input.
	},
	{
		Name:          "deepseek-v4-pro", // DeepSeek-V4-Pro-0813
		BaseURL:       "https://api.deepseek.com",
		ContextWindow: 1_000_000,
		// V4-Pro has no image understanding.
	},
}
