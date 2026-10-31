package mistral

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived regressions, not upstream test ports: pinned Mistral's
// getMistralCachedPromptTokens declares six nullish aliases. The upstream HTTP
// test exercises only the first snake-case form. Fixtures use integral counts.
func TestMistralUsageAliasesSourceMain(t *testing.T) {
	raw, err := os.ReadFile("testdata/usage_alias_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string
		Aliases   map[string]any
		CacheRead int
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			usage := tc.Aliases
			usage["prompt_tokens"] = 10
			usage["completion_tokens"] = 4
			model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				upstreamMistralWrite(w, map[string]any{"id": "usage-response", "choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}}, "usage": usage})
			})
			model.Cost = goai.ModelCost{Input: 2, Output: 4, CacheRead: 1}
			message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test"})
			if message.StopReason != goai.StopReasonStop || message.Usage == nil {
				t.Fatalf("unexpected result %#v", message)
			}
			got := message.Usage
			if got.Input != 10-tc.CacheRead || got.Output != 4 || got.CacheRead != tc.CacheRead || got.CacheWrite != 0 || got.TotalTokens != 14 {
				t.Fatalf("usage %#v; want input=%d output=4 cacheRead=%d cacheWrite=0 total=14", got, 10-tc.CacheRead, tc.CacheRead)
			}
			// Independent rate arithmetic; 1e-18 exceeds roundoff for these <5e-5 USD totals.
			want := float64(2*(10-tc.CacheRead)+4*4+tc.CacheRead) / 1e6
			if math.IsNaN(got.Cost.Total) || math.IsInf(got.Cost.Total, 0) || math.Abs(got.Cost.Total-want) > 1e-18 {
				t.Fatalf("cost %.20g want %.20g", got.Cost.Total, want)
			}
		})
	}
}
