package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestClampMaxTokensToContextUsesEstimateAndSafetyWindow(t *testing.T) {
	model := &goai.Model{ContextWindow: 5000, MaxTokens: 2000}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	// estimate("hello") = 2, so available = 5000 - 2 - 4096 = 902.
	if got := goai.ClampMaxTokensToContext(model, ctx, 2000); got != 902 {
		t.Fatalf("clamped max tokens=%d, want 902", got)
	}
}

func TestClampMaxTokensToContextHonorsMinimumAndUnboundedModels(t *testing.T) {
	if got := goai.ClampMaxTokensToContext(&goai.Model{ContextWindow: 0}, &goai.Context{}, -10); got != 1 {
		t.Fatalf("unbounded/min clamp=%d, want 1", got)
	}
	if got := goai.ClampMaxTokensToContext(&goai.Model{ContextWindow: 4096}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, 100); got != 1 {
		t.Fatalf("exhausted context clamp=%d, want 1", got)
	}
}
