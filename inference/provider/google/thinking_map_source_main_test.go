package google

// Source-derived production-path probes, not additional translated upstream
// test cases. Pinned google-shared.ts#getDisabledGoogleThinkingConfig and both
// adapters' streamSimple/getGoogleBudget control flow at 4b535d57.
import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMainSourceGoogleThinkingControlFlow(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			name, reasoning, id string
			mapping             map[goai.ModelThinkingLevel]*string
			budgets             *goai.ThinkingBudgets
			want                map[string]any
		}{
			{name: "omitted with off supported", id: "gemini-3.7-flash", want: map[string]any{"thinkingBudget": float64(0)}},
			{name: "explicit off", id: "gemini-3.7-flash", reasoning: "off", want: map[string]any{"thinkingBudget": float64(0)}},
			{name: "case insensitive Gemini 4 control", id: "GEMINI-4-argon", reasoning: "medium", want: map[string]any{"includeThoughts": true, "thinkingLevel": "MEDIUM"}},
			{name: "unsupported low clamps upward", id: "gemini-3.7-flash", reasoning: "low", mapping: map[goai.ModelThinkingLevel]*string{"low": nil}, want: map[string]any{"includeThoughts": true, "thinkingLevel": "MEDIUM"}},
			{name: "explicit zero custom budget", id: "gemini-2.5-flash", reasoning: "high", budgets: &goai.ThinkingBudgets{High: new(int)}, want: map[string]any{"includeThoughts": true, "thinkingBudget": float64(0)}},
		} {
			t.Run(fmt.Sprint(api)+"/"+tc.name, func(t *testing.T) {
				got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(api, tc.id, tc.mapping), tc.reasoning, tc.budgets)
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("thinking %#v want %#v", got, tc.want)
				}
			})
		}
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reject invalid enabled=%t", api, enabled), func(t *testing.T) {
				level := "minimal"
				mapping := map[goai.ModelThinkingLevel]*string{"off": nil, "minimal": upstreamGoogleString("extreme")}
				opts := &goai.StreamOptions{APIKey: "test"}
				if enabled {
					level = "high"
					mapping = map[goai.ModelThinkingLevel]*string{"high": upstreamGoogleString("extreme")}
					reasoning := goai.ThinkingHigh
					opts.Reasoning = &reasoning
				}
				model := upstreamGoogleThinkingModel(api, "gemini-3.7-flash", mapping)
				called := false
				opts.OnPayload = func(any, *goai.Model) (any, error) { called = true; return nil, errors.New("unexpected payload") }
				want := fmt.Sprintf("Unsupported Google thinking level mapping for %s/%s: %s -> extreme", model.Provider, model.ID, level)
				count := 0
				for event := range goai.GetApiProvider(api).StreamSimple(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, opts) {
					if e, ok := event.(*goai.ErrorEvent); ok {
						count++
						if e.Err == nil || e.Err.Error() != want {
							t.Errorf("error %v want %s", e.Err, want)
						}
					}
				}
				if called || count != 1 {
					t.Fatalf("invalid map reached payload=%t or error count=%d", called, count)
				}
			})
		}
	}
}
