package goai_test

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestCompleteNilModelDoesNotPanic(t *testing.T) {
	msg, err := goai.Complete(context.Background(), nil, &goai.Context{}, nil)
	if err == nil || msg != nil {
		t.Fatalf("expected nil-model error and no message, got msg=%#v err=%v", msg, err)
	}
}

func TestNilRegistrationNoops(t *testing.T) {
	goai.RegisterApi(nil)
	goai.RegisterModel(nil)
	goai.RegisterApi(&goai.ApiProvider{})
	goai.RegisterModel(&goai.Model{})
}

func TestCloneContextDeepCopiesNestedFields(t *testing.T) {
	ctx := &goai.Context{Messages: []goai.Message{{
		Role: goai.RoleAssistant,
		Content: []goai.ContentBlock{{Type: "toolCall", ID: "tc", Name: "tool", Arguments: map[string]interface{}{
			"nested": map[string]interface{}{"value": "original"},
			"list":   []interface{}{map[string]interface{}{"item": "original"}},
		}}},
		Diagnostics: []goai.AssistantMessageDiagnostic{{Type: "diag", Details: map[string]interface{}{"nested": map[string]interface{}{"value": "original"}}}},
		Details:     map[string]interface{}{"nested": map[string]interface{}{"value": "original"}},
	}}}
	clone := goai.CloneContext(ctx)
	clone.Messages[0].Content[0].Arguments["nested"].(map[string]interface{})["value"] = "changed"
	clone.Messages[0].Content[0].Arguments["list"].([]interface{})[0].(map[string]interface{})["item"] = "changed"
	clone.Messages[0].Diagnostics[0].Details["nested"].(map[string]interface{})["value"] = "changed"
	clone.Messages[0].Details.(map[string]interface{})["nested"].(map[string]interface{})["value"] = "changed"

	origArgs := ctx.Messages[0].Content[0].Arguments
	if got := origArgs["nested"].(map[string]interface{})["value"]; got != "original" {
		t.Fatalf("nested arguments aliased: %v", got)
	}
	if got := origArgs["list"].([]interface{})[0].(map[string]interface{})["item"]; got != "original" {
		t.Fatalf("slice arguments aliased: %v", got)
	}
	if got := ctx.Messages[0].Diagnostics[0].Details["nested"].(map[string]interface{})["value"]; got != "original" {
		t.Fatalf("diagnostics aliased: %v", got)
	}
	if got := ctx.Messages[0].Details.(map[string]interface{})["nested"].(map[string]interface{})["value"]; got != "original" {
		t.Fatalf("details aliased: %v", got)
	}
}

func TestGetToolCallsReturnsArgumentCopies(t *testing.T) {
	msg := &goai.Message{Content: []goai.ContentBlock{{Type: "toolCall", ID: "tc", Name: "tool", Arguments: map[string]interface{}{"nested": map[string]interface{}{"value": "original"}}}}}
	calls := goai.GetToolCalls(msg)
	calls[0].Arguments["nested"].(map[string]interface{})["value"] = "changed"
	if got := msg.Content[0].Arguments["nested"].(map[string]interface{})["value"]; got != "original" {
		t.Fatalf("tool call arguments aliased: %v", got)
	}
}

func TestMapThinkingAndCostNilSafe(t *testing.T) {
	if got, ok := goai.MapThinkingLevel(nil, goai.ModelThinkingLevel(goai.ThinkingHigh)); !ok || got != "none" {
		t.Fatalf("nil model should clamp unsupported high reasoning to off, got %q ok=%v", got, ok)
	}
	if got, ok := goai.MapThinkingLevel(nil, goai.ThinkingOff); !ok || got != "none" {
		t.Fatalf("nil model off mapping = %q ok=%v", got, ok)
	}
	if cost := goai.CalculateCost(nil, &goai.Usage{Input: 1}); cost.Total != 0 {
		t.Fatalf("nil model cost = %#v", cost)
	}
	if cost := goai.CalculateCost(&goai.Model{}, nil); cost.Total != 0 {
		t.Fatalf("nil usage cost = %#v", cost)
	}
}

func TestAdjustMaxTokensForThinkingReservesOutput(t *testing.T) {
	maxTokens, thinkingBudget := goai.AdjustMaxTokensForThinking(100, 1200, goai.ThinkingHigh, nil)
	if maxTokens != 1200 || thinkingBudget != 176 {
		t.Fatalf("expected maxTokens=1200 thinkingBudget=176, got %d/%d", maxTokens, thinkingBudget)
	}
	maxTokens, thinkingBudget = goai.AdjustMaxTokensForThinking(100, 500, goai.ThinkingHigh, nil)
	if maxTokens != 500 || thinkingBudget != 0 {
		t.Fatalf("expected small budget to reserve all output, got %d/%d", maxTokens, thinkingBudget)
	}
}

func TestIsContextOverflowUsesDiagnosticsAndNilSafe(t *testing.T) {
	if goai.IsContextOverflow(nil, 1000) {
		t.Fatal("nil message should not overflow")
	}
	msg := &goai.Message{StopReason: goai.StopReasonError, Diagnostics: []goai.AssistantMessageDiagnostic{{Error: goai.DiagnosticError{Code: "context_length_exceeded", Message: "wrapped provider error"}}}}
	if !goai.IsContextOverflow(msg, 1000) {
		t.Fatal("expected diagnostic overflow code to be detected")
	}
	msg = &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "rate limit: too many requests and too many tokens"}
	if goai.IsContextOverflow(msg, 1000) {
		t.Fatal("non-overflow rate limit should win")
	}
}
