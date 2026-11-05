package goai

import (
	"strings"
	"testing"
)

// The pinned Decisions parser checks the answer type and choice before
// examining probabilities; preserve that error ordering for malformed input.
func TestMainDecisionsChoiceDiagnosticPrecedence(t *testing.T) {
	ctx := ClassifierContext{Questions: map[string]ClassifierQuestion{"q": {Type: "choice"}}}
	for _, tc := range []struct {
		name   string
		answer map[string]any
		want   string
	}{
		{"wrong-type-and-missing-probabilities", map[string]any{"name": "q", "type": "score"}, "did not return a choice answer for q"},
		{"missing-choice-and-invalid-probabilities", map[string]any{"name": "q", "type": "choice", "probabilities": nil}, "did not return a choice answer for q"},
		{"choice-valid-invalid-probabilities", map[string]any{"name": "q", "type": "choice", "choice": "a", "probabilities": nil}, "returned invalid probabilities for q"},
		{"refusal-before-everything", map[string]any{"name": "q", "type": "refusal"}, "refused to answer q"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDecisionsAnswers([]any{tc.answer}, ctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %q", err, tc.want)
			}
		})
	}
}
