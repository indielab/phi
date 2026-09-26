package optimizer

import (
	"os"
	"strings"

	"github.com/pulseaiclub/phi/internal/llm/jev"
)

// EnvOptimizer is the single switch for every optimizer feature: PHI_OPTIMIZER
// set to one of the "off" values keeps callers on their pre-optimizer behavior
// even where credentials exist. Feature flags live here rather than in the
// package that consumes a decision, so one value turns the whole subsystem off.
const EnvOptimizer = "PHI_OPTIMIZER"

// Available reports whether an optimizer feature may be wired up at all: the
// switch is on and a TypeSafe API key is configured. Optimizer features answer
// through a Jev judge, so with no key there is nothing to ask and the feature
// stays off instead of presenting UI that can never fill.
func Available() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvOptimizer))) {
	case "0", "false", "off", "no":
		return false
	}
	return jev.APIKeyConfigured()
}
