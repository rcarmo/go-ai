package goai_test

import (
	"strings"
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

func TestSourceV170ContextEstimateAndOutputClamp(t *testing.T) {
	model := &goai.Model{ID: "test-model", ContextWindow: 10_000, MaxTokens: 8_000}
	assistant := func(timestamp int64, total int) goai.Message {
		return goai.Message{
			Role: goai.RoleAssistant, Timestamp: timestamp, StopReason: goai.StopReasonStop,
			Content: []goai.ContentBlock{{Type: "text", Text: "kept"}},
			Usage:   &goai.Usage{TotalTokens: total},
		}
	}

	ctx := &goai.Context{Messages: []goai.Message{
		assistant(100, 2_000),
		{Role: goai.RoleUser, Timestamp: 200, Content: []goai.ContentBlock{{Type: "text", Text: strings.Repeat("x", 3_500)}}},
	}}
	est := goai.EstimateContextTokens(ctx)
	if est.Tokens != 3_000 || est.UsageTokens != 2_000 || est.TrailingTokens != 1_000 || est.LastUsageIndex == nil || *est.LastUsageIndex != 0 {
		t.Fatalf("new-text estimate=%#v", est)
	}
	if got := goai.ClampMaxTokensToContext(model, ctx, model.MaxTokens); got != 2_904 {
		t.Fatalf("new-text maxTokens=%d, want 2904", got)
	}

	stale := &goai.Context{SystemPrompt: "system", Messages: []goai.Message{
		{Role: goai.RoleUser, Timestamp: 200, Content: []goai.ContentBlock{{Type: "text", Text: "summary"}}},
		assistant(100, 9_500),
		{Role: goai.RoleUser, Timestamp: 300, Content: []goai.ContentBlock{{Type: "text", Text: strings.Repeat("x", 4_000)}}},
	}}
	est = goai.EstimateContextTokens(stale)
	if est.Tokens != 1_149 || est.UsageTokens != 0 || est.TrailingTokens != 1_149 || est.LastUsageIndex != nil {
		t.Fatalf("stale-usage estimate=%#v", est)
	}
	if got := goai.ClampMaxTokensToContext(model, stale, model.MaxTokens); got != 4_755 {
		t.Fatalf("stale-usage maxTokens=%d, want 4755", got)
	}

	anchored := &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleUser, Timestamp: 200, Content: []goai.ContentBlock{{Type: "text", Text: "summary"}}},
		assistant(100, 9_500),
		{Role: goai.RoleUser, Timestamp: 300, Content: []goai.ContentBlock{{Type: "text", Text: "new prompt"}}},
		assistant(400, 2_000),
		{Role: goai.RoleUser, Timestamp: 500, Content: []goai.ContentBlock{{Type: "text", Text: "tail"}}},
	}}
	est = goai.EstimateContextTokens(anchored)
	if est.Tokens != 2_002 || est.UsageTokens != 2_000 || est.TrailingTokens != 2 || est.LastUsageIndex == nil || *est.LastUsageIndex != 3 {
		t.Fatalf("renewed-usage estimate=%#v", est)
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
