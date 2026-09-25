package composer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm/jev"
	"github.com/pulseaiclub/phi/internal/optimizer/suggest"
	"github.com/pulseaiclub/phi/internal/session/shellhist"
	"github.com/pulseaiclub/phi/internal/tui/controller"
)

// fakeJudge answers the suggest package's two questions with a fixed
// distribution and gate, and records whether it was asked at all.
type fakeJudge struct {
	probabilities map[string]float64
	hasCompletion float64
	asked         int
}

func (f *fakeJudge) Evaluate(_ context.Context, _ any, _ jev.Questions) (jev.Response, error) {
	f.asked++
	return jev.Response{
		Model: "jev-test",
		Answers: map[string]jev.Answer{
			"completion": {
				Type:          jev.AnswerChoice,
				Choice:        "C00",
				Probabilities: f.probabilities,
			},
			"has_completion": {Type: jev.AnswerNoul, Noul: f.hasCompletion},
		},
	}, nil
}

// historyStore writes commands to a real shell-history store, oldest first, so
// the predictor is exercised over the file layout it reads in production.
func historyStore(t *testing.T, commands ...string) *shellhist.Store {
	t.Helper()
	store := shellhist.New(t.TempDir())
	for i, command := range commands {
		require.NoError(t, store.Append(shellhist.Entry{
			Version: 1,
			At:      int64(i + 1),
			Cwd:     "/tmp",
			Command: command,
		}))
	}
	return store
}

func newPredictor(t *testing.T, store *shellhist.Store, judge *fakeJudge) *BashHistoryPredictor {
	t.Helper()
	suggester, err := suggest.New(judge)
	require.NoError(t, err)
	return NewBashHistoryPredictor(store, suggester)
}

// The picker lists a ranking, not one answer: every literal prefix match is a
// completion, and the judge's distribution orders them.
func TestPredictListsEveryPrefixMatch(t *testing.T) {
	judge := &fakeJudge{
		probabilities: map[string]float64{"C00": 0.2, "C01": 0.8},
		hasCompletion: 0.95,
	}
	// Newest first once read back, so C00 is "git status".
	predictor := newPredictor(t, historyStore(t, "ls -la", "git stash", "git status"), judge)

	items, err := predictor.Predict(t.Context(), "git s")

	require.NoError(t, err)
	assert.Equal(t, []controller.BashSuggestion{
		{Command: "git stash", Score: 0.8, IsPrefix: true},
		{Command: "git status", Score: 0.2, IsPrefix: true},
	}, items)
}

// One literal prefix match is exact: there is nothing for a judge to decide.
func TestPredictSkipsTheJudgeForASinglePrefixMatch(t *testing.T) {
	judge := &fakeJudge{}
	predictor := newPredictor(t, historyStore(t, "ls -la", "git stash"), judge)

	items, err := predictor.Predict(t.Context(), "git")

	require.NoError(t, err)
	assert.Equal(t, []controller.BashSuggestion{{Command: "git stash", Score: 1, IsPrefix: true}}, items)
	assert.Zero(t, judge.asked, "an exact rule needs no judgement")
}

// Fuzzy rows below the minimum score are guesses the gate already rejected.
func TestPredictDropsRowsBelowTheMinimumScore(t *testing.T) {
	judge := &fakeJudge{
		probabilities: map[string]float64{"C00": 0.7, "C01": 0.4, "C02": 0.05},
		hasCompletion: 0.6,
	}
	predictor := newPredictor(t, historyStore(t, "make build", "npm test", "kubectl get pods"), judge)

	items, err := predictor.Predict(t.Context(), "ktl")

	require.NoError(t, err)
	assert.Equal(t, []controller.BashSuggestion{
		{Command: "kubectl get pods", Score: 0.7},
		{Command: "npm test", Score: 0.4},
	}, items, "the weak tail is dropped; fuzzy rows are not prefix matches")
}

func TestPredictStaysQuietWhenTheGatesRejectTheRanking(t *testing.T) {
	judge := &fakeJudge{probabilities: map[string]float64{"C00": 0.4}, hasCompletion: 0.1}
	predictor := newPredictor(t, historyStore(t, "make build"), judge)

	items, err := predictor.Predict(t.Context(), "zzz")

	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, 1, judge.asked, "the gate is what rejected it, not the missing request")
}

func TestPredictWithoutHistoryIsSilent(t *testing.T) {
	judge := &fakeJudge{}
	predictor := newPredictor(t, historyStore(t), judge)

	items, err := predictor.Predict(t.Context(), "git")

	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Zero(t, judge.asked, "an empty history is nothing to ask about")
}

// An inert predictor is not an error: the composer installs one only when it
// has both a history directory and a judge, and must never panic if it does not.
func TestPredictWithoutAStoreOrJudgeIsInert(t *testing.T) {
	for _, tt := range []struct {
		name      string
		predictor *BashHistoryPredictor
	}{
		{"nil predictor", nil},
		{"no store", NewBashHistoryPredictor(nil, nil)},
		{"store only", NewBashHistoryPredictor(historyStore(t, "git stash"), nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			items, err := tt.predictor.Predict(t.Context(), "git")

			require.NoError(t, err)
			assert.Empty(t, items)
		})
	}
}

// The picker is gated on the judge: an unset key is what keeps it closed on a
// build without TypeSafe credentials, and it is not an error the user sees.
func TestNewJevSuggesterRequiresAnAPIKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")

	suggester, err := NewJevSuggester()

	assert.Error(t, err)
	assert.Nil(t, suggester)

	t.Setenv("TYPESAFE_API_KEY", "test-key")

	suggester, err = NewJevSuggester()
	require.NoError(t, err)
	assert.NotNil(t, suggester)
}

// Wiring the picker takes two gates, and either one off leaves "!" on its
// pre-completion behavior: no client is built and no request is ever made.
func TestShellCompletionEnabled(t *testing.T) {
	for _, tt := range []struct {
		name     string
		apiKey   string
		flag     string
		expected bool
	}{
		{"key and no flag", "test-key", "", true},
		{"key and flag on", "test-key", "on", true},
		{"key and flag on in mixed case", "test-key", "ON", true},
		{"no key", "", "", false},
		{"blank key", "   ", "", false},
		{"flag off", "test-key", "off", false},
		{"flag zero", "test-key", "0", false},
		{"flag false in mixed case", "test-key", "False", false},
		{"flag off and no key", "", "off", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TYPESAFE_API_KEY", tt.apiKey)
			t.Setenv(envShellCompletion, tt.flag)

			assert.Equal(t, tt.expected, ShellCompletionEnabled())
		})
	}
}
