package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestEstimateTextTokensUsesCeilThreePointFiveCharsPerToken(t *testing.T) {
	for text, want := range map[string]int{"": 0, "abcd": 2, "abcdefg": 2, "abcdefgh": 3, "€€€€": 2, "😀😀": 2} {
		if got := goai.EstimateTextTokens(text); got != want {
			t.Fatalf("estimate %q=%d want %d", text, got, want)
		}
	}
	if got := goai.EstimateTextTokens("hello"); got != 2 {
		t.Fatalf("EstimateTextTokens(5 chars)=%d, want 2", got)
	}
}

func TestEstimateMessageTokensCountsImagesAsUpstreamChars(t *testing.T) {
	msg := goai.Message{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "image", Data: "x", MimeType: "image/png"}}}
	if got := goai.EstimateMessageTokens(msg); got != 1372 {
		t.Fatalf("image token estimate=%d, want 1372", got)
	}
}

func TestEstimateContextTokensUsesLastSuccessfulAssistantUsageAsAnchor(t *testing.T) {
	ctx := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("ignored older text"),
		{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, Usage: &goai.Usage{TotalTokens: 999}, ErrorMessage: "failed"},
		{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Usage: &goai.Usage{Input: 10, Output: 5, CacheRead: 2, CacheWrite: 3}},
		goai.UserMessage("hello"),
	}}
	est := goai.EstimateContextTokens(ctx)
	if est.UsageTokens != 20 || est.TrailingTokens != 2 || est.Tokens != 22 {
		t.Fatalf("estimate=%#v, want usage=20 trailing=2 total=22", est)
	}
	if est.LastUsageIndex == nil || *est.LastUsageIndex != 2 {
		t.Fatalf("last usage index=%v, want 2", est.LastUsageIndex)
	}
}

func TestEstimateContextTokensIncludesSystemAndToolsOnlyWithoutUsageAnchor(t *testing.T) {
	ctx := &goai.Context{
		SystemPrompt: "abcd",
		Messages:     []goai.Message{goai.UserMessage("hello")},
		Tools:        []goai.Tool{{Name: "tool", Description: "desc", Parameters: []byte(`{"type":"object"}`)}},
	}
	est := goai.EstimateContextTokens(ctx)
	if est.UsageTokens != 0 || est.LastUsageIndex != nil {
		t.Fatalf("unexpected usage anchor: %#v", est)
	}
	if est.Tokens <= 3 {
		t.Fatalf("expected system/tool prefix to be counted, got %#v", est)
	}
}
