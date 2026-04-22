package goai_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestCloneContext(t *testing.T) {
	ctx := &goai.Context{
		SystemPrompt: "You are helpful.",
		Messages: []goai.Message{
			goai.UserMessage("hello"),
			{
				Role: goai.RoleAssistant,
				Content: []goai.ContentBlock{
					{Type: "text", Text: "Hi there!"},
					{Type: "toolCall", ID: "tc_1", Name: "search", Arguments: map[string]interface{}{"q": "test"}},
				},
				Usage: &goai.Usage{Input: 100, Output: 50},
			},
		},
		Tools: []goai.Tool{{
			Name:        "search",
			Description: "Search",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
	}

	clone := goai.CloneContext(ctx)

	// Mutate clone — should not affect original
	clone.SystemPrompt = "CHANGED"
	clone.Messages[0].Content[0].Text = "CHANGED"
	clone.Messages[1].Content[1].Arguments["q"] = "CHANGED"
	clone.Messages[1].Usage.Input = 999
	clone.Tools[0].Name = "CHANGED"

	if ctx.SystemPrompt != "You are helpful." {
		t.Fatal("original system prompt was mutated")
	}
	if ctx.Messages[0].Content[0].Text != "hello" {
		t.Fatal("original user message was mutated")
	}
	if ctx.Messages[1].Content[1].Arguments["q"] != "test" {
		t.Fatal("original tool call arguments were mutated")
	}
	if ctx.Messages[1].Usage.Input != 100 {
		t.Fatal("original usage was mutated")
	}
	if ctx.Tools[0].Name != "search" {
		t.Fatal("original tool name was mutated")
	}
}

func TestCloneContextNil(t *testing.T) {
	if goai.CloneContext(nil) != nil {
		t.Fatal("clone of nil should be nil")
	}
}

func TestSaveLoadContext(t *testing.T) {
	ctx := &goai.Context{
		SystemPrompt: "test prompt",
		Messages:     []goai.Message{goai.UserMessage("hello")},
	}

	path := filepath.Join(t.TempDir(), "context.json")
	if err := goai.SaveContext(ctx, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := goai.LoadContext(path)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.SystemPrompt != "test prompt" {
		t.Fatal("system prompt mismatch")
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content[0].Text != "hello" {
		t.Fatal("message mismatch")
	}

	// Verify the file is valid JSON
	data, _ := os.ReadFile(path)
	if !json.Valid(data) {
		t.Fatal("saved file is not valid JSON")
	}
}

func TestEstimateTokens(t *testing.T) {
	ctx := &goai.Context{
		SystemPrompt: "You are a helpful assistant.",
		Messages: []goai.Message{
			goai.UserMessage("What is the meaning of life?"),
		},
	}

	tokens := goai.EstimateTokens(ctx)
	if tokens <= 0 {
		t.Fatalf("expected positive token estimate, got %d", tokens)
	}
	// ~7 + ~7 + 4 overhead ≈ 18
	if tokens > 100 {
		t.Fatalf("token estimate seems too high: %d", tokens)
	}
}

func TestFitsInContextWindow(t *testing.T) {
	ctx := &goai.Context{
		SystemPrompt: "short",
		Messages:     []goai.Message{goai.UserMessage("hi")},
	}

	model := &goai.Model{ContextWindow: 128000}
	fits, tokens := goai.FitsInContextWindow(ctx, model)
	if !fits {
		t.Fatalf("should fit, got %d tokens", tokens)
	}

	tinyModel := &goai.Model{ContextWindow: 1}
	fits, _ = goai.FitsInContextWindow(ctx, tinyModel)
	if fits {
		t.Fatal("should not fit in 1-token window")
	}
}

func TestCompactContext(t *testing.T) {
	// Build a context with many messages
	ctx := &goai.Context{
		SystemPrompt: "You are helpful.",
	}
	for i := 0; i < 100; i++ {
		ctx.Messages = append(ctx.Messages, goai.UserMessage("message"))
	}

	model := &goai.Model{ContextWindow: 50} // tiny window
	compacted := goai.CompactContext(ctx, model, 5)

	if len(compacted.Messages) > 5 {
		t.Fatalf("expected at most 5 messages, got %d", len(compacted.Messages))
	}
	if compacted.SystemPrompt != "You are helpful." {
		t.Fatal("system prompt should be preserved")
	}

	// Original should not be mutated
	if len(ctx.Messages) != 100 {
		t.Fatal("original context was mutated")
	}
}

func TestGetToolCalls(t *testing.T) {
	msg := &goai.Message{
		Content: []goai.ContentBlock{
			{Type: "text", Text: "Let me search for that."},
			{Type: "toolCall", ID: "tc_1", Name: "search", Arguments: map[string]interface{}{"q": "test"}},
			{Type: "toolCall", ID: "tc_2", Name: "read", Arguments: map[string]interface{}{"path": "/tmp"}},
		},
	}

	calls := goai.GetToolCalls(msg)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "search" || calls[1].Name != "read" {
		t.Fatal("tool call names mismatch")
	}
}

func TestNeedsToolExecution(t *testing.T) {
	// Assistant with tool calls and toolUse stop reason
	msg := &goai.Message{
		Role:       goai.RoleAssistant,
		StopReason: goai.StopReasonToolUse,
		Content:    []goai.ContentBlock{{Type: "toolCall", ID: "tc_1", Name: "search"}},
	}
	if !goai.NeedsToolExecution(msg) {
		t.Fatal("should need tool execution")
	}

	// Text-only assistant
	msg2 := &goai.Message{
		Role:       goai.RoleAssistant,
		StopReason: goai.StopReasonStop,
		Content:    []goai.ContentBlock{{Type: "text", Text: "done"}},
	}
	if goai.NeedsToolExecution(msg2) {
		t.Fatal("should not need tool execution")
	}
}

func TestAppendHelpers(t *testing.T) {
	ctx := &goai.Context{}

	goai.AppendUserMessage(ctx, "hello")
	if len(ctx.Messages) != 1 || ctx.Messages[0].Role != goai.RoleUser {
		t.Fatal("AppendUserMessage failed")
	}

	goai.AppendToolResult(ctx, "tc_1", "search", "result text", false)
	if len(ctx.Messages) != 2 || ctx.Messages[1].Role != goai.RoleToolResult {
		t.Fatal("AppendToolResult failed")
	}
	if ctx.Messages[1].ToolCallID != "tc_1" {
		t.Fatal("tool call ID mismatch")
	}
}

func TestHooksOnStreamOptions(t *testing.T) {
	// Verify hooks can be set on StreamOptions
	called := false
	opts := &goai.StreamOptions{
		OnPayload: func(payload interface{}, model *goai.Model) (interface{}, error) {
			called = true
			return payload, nil
		},
	}

	result, err := goai.InvokeOnPayload(opts, map[string]interface{}{"test": true}, &goai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("OnPayload was not called")
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
}

func TestInvokeOnPayloadNil(t *testing.T) {
	// Nil opts should pass through
	result, err := goai.InvokeOnPayload(nil, "payload", &goai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	if result != "payload" {
		t.Fatal("should pass through unchanged")
	}
}
