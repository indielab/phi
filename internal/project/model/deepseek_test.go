package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm"
)

func TestLookupDeepSeekFlash(t *testing.T) {
	p, ok := Lookup("deepseek-flash")
	require.True(t, ok)
	assert.Equal(t, "deepseek-flash", p.Name)
	assert.Equal(t, "https://api.deepseek.com", p.BaseURL)
	assert.Equal(t, 1_000_000, p.ContextWindow)
	assert.True(t, p.ImageEnabled, "V4.1-Flash accepts image input")
}

func TestLookupDeepSeekV4Pro(t *testing.T) {
	p, ok := Lookup("deepseek-v4-pro")
	require.True(t, ok)
	assert.Equal(t, "deepseek-v4-pro", p.Name)
	assert.Equal(t, "https://api.deepseek.com", p.BaseURL)
	assert.Equal(t, 1_000_000, p.ContextWindow)
	assert.False(t, p.ImageEnabled, "V4-Pro has no image understanding")
}

func TestLookupUnknownFallsThrough(t *testing.T) {
	// Legacy / custom names are not presets; callers apply the generic
	// OpenAI default themselves.
	_, ok := Lookup("deepseek-chat")
	assert.False(t, ok)
	_, ok = Lookup("gpt-4o")
	assert.False(t, ok)

	// A preset never leaks api_key or skill path — those stay caller-owned.
	p, ok := Lookup("deepseek-flash")
	require.True(t, ok)
	assert.Empty(t, p.APIKey)
	assert.Empty(t, p.SkillPath)
	assert.Equal(t, llm.ModelConfig{
		Name:          "deepseek-flash",
		BaseURL:       "https://api.deepseek.com",
		ContextWindow: 1_000_000,
		ImageEnabled:  true,
	}, p)
}
