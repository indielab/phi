package optimizer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Wiring an optimizer feature takes two gates, and either one off leaves it on
// its pre-optimizer behavior: no judge is built and no request is ever made.
func TestAvailable(t *testing.T) {
	for _, tt := range []struct {
		name     string
		apiKey   string
		flag     string
		expected bool
	}{
		{"key and no flag", "test-key", "", true},
		{"key and flag on", "test-key", "on", true},
		{"key and flag on in mixed case", "test-key", "ON", true},
		{"key and flag on with padding", "test-key", " on ", true},
		{"key and unrecognized flag", "test-key", "whatever", true},
		{"no key", "", "", false},
		{"blank key", "   ", "", false},
		{"flag off", "test-key", "off", false},
		{"flag zero", "test-key", "0", false},
		{"flag no in mixed case", "test-key", "No", false},
		{"flag false in mixed case", "test-key", "False", false},
		{"flag off and no key", "", "off", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TYPESAFE_API_KEY", tt.apiKey)
			t.Setenv(EnvOptimizer, tt.flag)

			assert.Equal(t, tt.expected, Available())
		})
	}
}
