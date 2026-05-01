package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// --- Azure ---

func TestApplyToolCallLimitNoOp(t *testing.T) {
	msgs := make([]interface{}, 10)
	for i := range msgs {
		msgs[i] = map[string]interface{}{"type": "message", "role": "user"}
	}
	cfg := goai.DefaultToolCallLimitConfig()
	result := goai.ApplyToolCallLimit(msgs, cfg)
	if result.ToolCallRemoved != 0 {
		t.Fatalf("expected 0 removed, got %d", result.ToolCallRemoved)
	}
}

func TestApplyToolCallLimitTrims(t *testing.T) {
	var msgs []interface{}
	// Build 10 function_call + function_call_output pairs
	for i := 0; i < 10; i++ {
		msgs = append(msgs, map[string]interface{}{
			"type": "function_call", "name": "search", "call_id": i,
		})
		msgs = append(msgs, map[string]interface{}{
			"type": "function_call_output", "output": "result",
		})
	}

	cfg := goai.ToolCallLimitConfig{Limit: 5, SummaryMax: 1000, OutputChars: 50}
	result := goai.ApplyToolCallLimit(msgs, cfg)
	if result.ToolCallRemoved != 5 {
		t.Fatalf("expected 5 removed, got %d", result.ToolCallRemoved)
	}
	if result.SummaryText == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestAzureSessionHeaders(t *testing.T) {
	h := goai.AzureSessionHeaders("sess-123")
	if h["session_id"] != "sess-123" {
		t.Fatal("missing session_id")
	}
	if h["x-ms-client-request-id"] != "sess-123" {
		t.Fatal("missing x-ms-client-request-id")
	}

	if goai.AzureSessionHeaders("") != nil {
		t.Fatal("expected nil for empty session")
	}
}

func TestNormalizeAzureReasoningEventPassthrough(t *testing.T) {
	// Normal event should pass through unchanged
	event := map[string]interface{}{"type": "response.output_text.delta", "delta": "hello"}
	result := goai.NormalizeAzureReasoningEvent(event)
	if result["type"] != "response.output_text.delta" {
		t.Fatal("should pass through unchanged")
	}
}

func TestNormalizeAzureReasoningEventCommentary(t *testing.T) {
	event := map[string]interface{}{
		"type": "response.output_item.added",
		"item": map[string]interface{}{
			"id": "item_1", "type": "message", "phase": "commentary",
		},
	}
	result := goai.NormalizeAzureReasoningEvent(event)
	item, _ := result["item"].(map[string]interface{})
	if item["type"] != "reasoning" {
		t.Fatalf("expected reasoning, got %v", item["type"])
	}
}

func TestNormalizeAzureReasoningTextDelta(t *testing.T) {
	event := map[string]interface{}{"type": "response.reasoning_text.delta", "delta": "thinking..."}
	result := goai.NormalizeAzureReasoningEvent(event)
	if result["type"] != "response.reasoning_summary_text.delta" {
		t.Fatalf("expected normalized type, got %v", result["type"])
	}
}

// --- Compat ---

func TestDetectCompatProviders(t *testing.T) {
	tests := []struct {
		url     string
		devRole bool
		strict  bool
	}{
		{"https://api.openai.com/v1", true, true},
		{"http://localhost:11434/v1", false, false},  // Ollama
		{"https://api.groq.com/v1", true, true},      // Groq follows upstream's standard-compatible payload shape
		{"https://openrouter.ai/api/v1", true, true}, // OpenRouter follows upstream's standard-compatible payload shape
	}
	for _, tt := range tests {
		c := goai.DetectCompat(tt.url)
		if c.SupportsDeveloperRole != nil && *c.SupportsDeveloperRole != tt.devRole {
			t.Errorf("%s: SupportsDeveloperRole=%v, want %v", tt.url, *c.SupportsDeveloperRole, tt.devRole)
		}
	}
}

// --- Env ---

func TestResolveAPIKey(t *testing.T) {
	model := &goai.Model{Provider: "test", APIKey: "model-key"}

	// Option key takes precedence
	key := goai.ResolveAPIKey(model, &goai.StreamOptions{APIKey: "opt-key"})
	if key != "opt-key" {
		t.Fatalf("expected opt-key, got %s", key)
	}

	// Model key fallback
	key = goai.ResolveAPIKey(model, nil)
	if key != "model-key" {
		t.Fatalf("expected model-key, got %s", key)
	}

	// No key
	model2 := &goai.Model{Provider: "unknown"}
	key = goai.ResolveAPIKey(model2, nil)
	if key != "" {
		t.Fatalf("expected empty, got %s", key)
	}
}

// --- Transform ---

func TestTransformMessagesPreservesImages(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: "test", Api: "test", Input: []string{"text", "image"}}
	messages := []goai.Message{
		{Role: goai.RoleUser, Content: []goai.ContentBlock{
			{Type: "text", Text: "Look:"},
			{Type: "image", Data: "base64data", MimeType: "image/png"},
		}},
	}
	result := goai.TransformMessages(messages, model)
	if len(result[0].Content) != 2 {
		t.Fatal("should preserve images for vision model")
	}
	if result[0].Content[1].Type != "image" {
		t.Fatal("second block should still be image")
	}
}

func TestTransformInsertsSyntheticToolResults(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: "test", Api: "test", Input: []string{"text"}}
	messages := []goai.Message{
		goai.UserMessage("hi"),
		{
			Role:       goai.RoleAssistant,
			StopReason: goai.StopReasonToolUse,
			Content:    []goai.ContentBlock{{Type: "toolCall", ID: "tc_1", Name: "search"}},
			Provider:   "test", Api: "test", Model: "test",
		},
		// Missing tool result — should get synthetic one
		goai.UserMessage("continue"),
	}
	result := goai.TransformMessages(messages, model)
	// Should have: user, assistant, synthetic tool result, user
	foundToolResult := false
	for _, m := range result {
		if m.Role == goai.RoleToolResult && m.IsError {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		t.Fatal("expected synthetic tool result for orphaned tool call")
	}
}

// --- Simple options ---

func TestClampReasoning(t *testing.T) {
	if goai.ClampReasoning(goai.ThinkingXHigh) != goai.ThinkingHigh {
		t.Fatal("xhigh should clamp to high")
	}
	if goai.ClampReasoning(goai.ThinkingMedium) != goai.ThinkingMedium {
		t.Fatal("medium should stay medium")
	}
}

func TestSupportsXhigh(t *testing.T) {
	yes := &goai.Model{ID: "gpt-5.2-turbo"}
	no := &goai.Model{ID: "gpt-4o"}
	if !goai.SupportsXhigh(yes) {
		t.Fatal("gpt-5.2 should support xhigh")
	}
	if goai.SupportsXhigh(no) {
		t.Fatal("gpt-4o should not support xhigh")
	}
}

// --- Validate type coverage ---

func TestValidateTypeChecks(t *testing.T) {
	tools := []goai.Tool{{
		Name:        "test",
		Description: "test",
		Parameters:  []byte(`{"type":"object","properties":{"n":{"type":"number"},"b":{"type":"boolean"},"a":{"type":"array"},"o":{"type":"object"},"s":{"type":"string","enum":["x","y"]}},"required":["n"]}`),
	}}

	// Valid
	_, err := goai.ValidateToolCall(tools, goai.ToolCall{Name: "test", Arguments: map[string]interface{}{"n": 42.0, "b": true, "a": []interface{}{1}, "o": map[string]interface{}{}, "s": "x"}})
	if err != nil {
		t.Fatal(err)
	}

	// Wrong type
	_, err = goai.ValidateToolCall(tools, goai.ToolCall{Name: "test", Arguments: map[string]interface{}{"n": "not a number"}})
	if err == nil {
		t.Fatal("expected error for wrong type")
	}

	// Invalid enum
	_, err = goai.ValidateToolCall(tools, goai.ToolCall{Name: "test", Arguments: map[string]interface{}{"n": 1.0, "s": "z"}})
	if err == nil {
		t.Fatal("expected error for invalid enum")
	}

	// Wrong boolean
	_, err = goai.ValidateToolCall(tools, goai.ToolCall{Name: "test", Arguments: map[string]interface{}{"n": 1.0, "b": "not bool"}})
	if err == nil {
		t.Fatal("expected error for wrong boolean")
	}

	// Wrong array
	_, err = goai.ValidateToolCall(tools, goai.ToolCall{Name: "test", Arguments: map[string]interface{}{"n": 1.0, "a": "not array"}})
	if err == nil {
		t.Fatal("expected error for wrong array")
	}

	// Wrong object
	_, err = goai.ValidateToolCall(tools, goai.ToolCall{Name: "test", Arguments: map[string]interface{}{"n": 1.0, "o": "not object"}})
	if err == nil {
		t.Fatal("expected error for wrong object")
	}
}

// --- Registry ---

func TestUnregisterAndClear(t *testing.T) {
	goai.RegisterApi(&goai.ApiProvider{Api: "test-unreg", Stream: nil, StreamSimple: nil})
	if goai.GetApiProvider("test-unreg") == nil {
		t.Fatal("should be registered")
	}
	goai.UnregisterApi("test-unreg")
	if goai.GetApiProvider("test-unreg") != nil {
		t.Fatal("should be unregistered")
	}
}
