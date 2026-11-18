package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// All assertions in pi-ai/test/clamp-max-tokens.test.ts at v1.5.2.
func TestSourceV152ClampMaxTokensToContext(t *testing.T) {
	const window = 1_048_576
	for _, tc := range []struct {
		name                       string
		input, window, limit, want int
	}{
		{"fifteen percent margin", 220_815, window, 943_718, window - 220_815 - 33_123},
		{"fixed minimum margin", 10_000, window, window, window - 10_000 - 4096},
		{"model cap already fits", 100_000, window, 64_000, 64_000},
		{"near-full output cap", 912_000, window, 943_718, 32_768},
		{"real remaining room", 1_030_000, window, 943_718, window - 1_030_000 - 4096},
		{"small window", 20_000, 32_768, 32_768, 32_768 - 20_000 - 4096},
		{"exhausted context", 1_050_000, window, 943_718, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ContextWindow: tc.window, MaxTokens: tc.limit}
			user := goai.UserMessage("hi")
			user.Timestamp = 1
			ctx := &goai.Context{Messages: []goai.Message{user, {
				Role: goai.RoleAssistant, Timestamp: 2, StopReason: goai.StopReasonStop,
				Usage: &goai.Usage{Input: tc.input, TotalTokens: tc.input},
			}}}
			got := goai.ClampMaxTokensToContext(model, ctx, tc.limit)
			if got != tc.want {
				t.Fatalf("max tokens = %d; want %d", got, tc.want)
			}
			if tc.input == 220_815 && 229_747+got > window {
				t.Fatalf("overcounted request exceeds the window: %d", 229_747+got)
			}
		})
	}
}
