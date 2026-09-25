package composer

import (
	"context"
	"os"
	"strings"

	"github.com/pulseaiclub/phi/internal/llm/jev"
	"github.com/pulseaiclub/phi/internal/optimizer"
	"github.com/pulseaiclub/phi/internal/optimizer/suggest"
	"github.com/pulseaiclub/phi/internal/session/shellhist"
	"github.com/pulseaiclub/phi/internal/tui/controller"
)

// BashHistoryPredictor ranks "!" completions from the project shell history.
// shellhist owns reading and caching history.jsonl; suggest owns candidate
// selection and the Jev judgement; this type is only the seam between them and
// the composer's predictor interface.
type BashHistoryPredictor struct {
	store     *shellhist.Store
	suggester *suggest.Suggester
}

// NewBashHistoryPredictor combines a project history store with a Jev-backed
// suggester. A nil store or suggester leaves the predictor inert: Predict then
// reports no completions instead of panicking, so a caller can install it
// unconditionally.
func NewBashHistoryPredictor(store *shellhist.Store, suggester *suggest.Suggester) *BashHistoryPredictor {
	return &BashHistoryPredictor{store: store, suggester: suggester}
}

// envShellCompletion turns the "!" picker off outright: PHI_SHELL_COMPLETION=off
// keeps the composer on its pre-completion behavior even where credentials
// exist.
const envShellCompletion = "PHI_SHELL_COMPLETION"

// ShellCompletionEnabled reports whether "!" completions may be wired up at all:
// the feature flag is on and a TypeSafe API key is configured. Ranking
// candidates is a judgement call, so with no key there is no judge to consult
// and the feature stays off rather than showing a picker that can never fill.
func ShellCompletionEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envShellCompletion))) {
	case "0", "false", "off", "no":
		return false
	}
	return jev.APIKeyConfigured()
}

// NewJevSuggester builds a suggester over the TypeSafe System One backend.
// An unset TYPESAFE_API_KEY fails here, and the caller leaves the predictor
// unwired: without a judge the "!" picker stays closed.
func NewJevSuggester() (*suggest.Suggester, error) {
	client, err := jev.NewClient(jev.Config{})
	if err != nil {
		return nil, err
	}
	backend, err := optimizer.NewJevBackend(client)
	if err != nil {
		return nil, err
	}
	return suggest.New(backend)
}

// Predict ranks the project history against the typed text. A missing or
// unreadable history is an empty one: completions degrade to Jev-less silence
// instead of reporting an error the user cannot act on.
func (p *BashHistoryPredictor) Predict(ctx context.Context, typed string) ([]controller.BashSuggestion, error) {
	if p == nil || p.store == nil || p.suggester == nil {
		return nil, nil
	}
	history, err := p.store.Commands()
	if err != nil {
		history = nil
	}
	result, err := p.suggester.Suggest(ctx, typed, history)
	if err != nil {
		return nil, err
	}
	// The picker lists a ranking, not one answer: the judge already ordered the
	// candidates and Shortlist decides which of them cleared the gates.
	ranked := p.suggester.Shortlist(result, bashSuggestVisible)
	if len(ranked) == 0 {
		return nil, nil
	}
	items := make([]controller.BashSuggestion, 0, len(ranked))
	for _, suggestion := range ranked {
		items = append(items, controller.BashSuggestion{
			Command:  suggestion.Command,
			Score:    suggestion.Score,
			IsPrefix: suggestion.IsPrefix,
		})
	}
	return items, nil
}
