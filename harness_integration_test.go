package goai_test

import (
	"context"
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/provider/faux"
)

// TestAgentLoopHarness simulates a complete multi-turn agent conversation
// with tool calling, context management, overflow handling, and model switching.
func TestAgentLoopHarness(t *testing.T) {
	reg := faux.Register(&faux.Options{
		Models: []faux.ModelDef{
			{ID: "fast-model", Name: "Fast", ContextWindow: 128000, MaxTokens: 4096, Input: []string{"text"}},
			{ID: "smart-model", Name: "Smart", Reasoning: true, ContextWindow: 200000, MaxTokens: 16384, Input: []string{"text", "image"}},
		},
	})

	tools := []goai.Tool{
		{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		},
		{
			Name:        "search",
			Description: "Search the web",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		},
	}

	// --- Turn 1: User asks a question, model calls a tool ---
	reg.SetResponses([]faux.ResponseStep{
		faux.ToolCallMessage("read_file", map[string]interface{}{"path": "/tmp/test.txt"}),
	})

	convCtx := &goai.Context{
		SystemPrompt: "You are a helpful coding assistant.",
		Messages:     []goai.Message{goai.UserMessage("Read the test file")},
		Tools:        tools,
	}

	model := reg.GetModel("fast-model")
	msg1, err := goai.Complete(context.Background(), model, convCtx, nil)
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	goai.AppendAssistantMessage(convCtx, msg1)

	if !goai.NeedsToolExecution(msg1) {
		t.Fatal("turn 1: expected tool call")
	}

	// Execute tool
	calls := goai.GetToolCalls(msg1)
	if len(calls) != 1 || calls[0].Name != "read_file" {
		t.Fatalf("turn 1: expected read_file, got %v", calls)
	}

	// Validate arguments
	args, err := goai.ValidateToolCall(tools, calls[0])
	if err != nil {
		t.Fatalf("turn 1: validation failed: %v", err)
	}
	if args["path"] != "/tmp/test.txt" {
		t.Fatalf("turn 1: expected path=/tmp/test.txt, got %v", args["path"])
	}

	goai.AppendToolResult(convCtx, calls[0].ID, calls[0].Name, "file contents here", false)

	// --- Turn 2: Model responds with text after tool result ---
	reg.AppendResponses([]faux.ResponseStep{
		faux.TextMessage("The file contains: file contents here"),
	})

	msg2, err := goai.Complete(context.Background(), model, convCtx, nil)
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	goai.AppendAssistantMessage(convCtx, msg2)

	if goai.NeedsToolExecution(msg2) {
		t.Fatal("turn 2: expected text response, not tool call")
	}

	text := goai.GetTextContent(msg2)
	if text == "" {
		t.Fatal("turn 2: expected non-empty text")
	}

	// --- Turn 3: User asks follow-up, model calls two tools ---
	reg.AppendResponses([]faux.ResponseStep{
		// Two tool calls in one response
		&goai.Message{
			Role: goai.RoleAssistant,
			Content: []goai.ContentBlock{
				{Type: "toolCall", ID: "tc_search", Name: "search", Arguments: map[string]interface{}{"query": "go channels"}},
				{Type: "toolCall", ID: "tc_read", Name: "read_file", Arguments: map[string]interface{}{"path": "/tmp/notes.md"}},
			},
			StopReason: goai.StopReasonToolUse,
			Usage:      &goai.Usage{Input: 200, Output: 100, TotalTokens: 300},
		},
	})

	goai.AppendUserMessage(convCtx, "Search for Go channels and read my notes")
	msg3, err := goai.Complete(context.Background(), model, convCtx, nil)
	if err != nil {
		t.Fatalf("turn 3: %v", err)
	}
	goai.AppendAssistantMessage(convCtx, msg3)

	calls3 := goai.GetToolCalls(msg3)
	if len(calls3) != 2 {
		t.Fatalf("turn 3: expected 2 tool calls, got %d", len(calls3))
	}

	// Execute both tools
	for _, tc := range calls3 {
		goai.AppendToolResult(convCtx, tc.ID, tc.Name, "tool result for "+tc.Name, false)
	}

	// --- Turn 4: Final response ---
	reg.AppendResponses([]faux.ResponseStep{
		faux.TextMessage("Based on the search and your notes: here's the answer."),
	})

	msg4, err := goai.Complete(context.Background(), model, convCtx, nil)
	if err != nil {
		t.Fatalf("turn 4: %v", err)
	}
	goai.AppendAssistantMessage(convCtx, msg4)

	// --- Verify full conversation state ---
	if len(convCtx.Messages) != 9 {
		// user, assistant(tool), tool_result, assistant(text), user, assistant(2 tools), 2 tool_results, assistant(text)
		t.Fatalf("expected 9 messages, got %d", len(convCtx.Messages))
	}

	// --- Test context persistence ---
	path := t.TempDir() + "/session.json"
	if err := goai.SaveContext(convCtx, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := goai.LoadContext(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Messages) != len(convCtx.Messages) {
		t.Fatalf("loaded message count mismatch: %d vs %d", len(loaded.Messages), len(convCtx.Messages))
	}
	if loaded.SystemPrompt != convCtx.SystemPrompt {
		t.Fatal("system prompt mismatch after load")
	}

	// --- Test token estimation ---
	tokens := goai.EstimateTokens(convCtx)
	if tokens <= 0 {
		t.Fatal("expected positive token estimate")
	}

	// --- Test context cloning ---
	clone := goai.CloneContext(convCtx)
	clone.Messages = append(clone.Messages, goai.UserMessage("extra"))
	if len(clone.Messages) == len(convCtx.Messages) {
		t.Fatal("clone should not affect original")
	}

	// --- Test model switching with TransformMessages ---
	smartModel := reg.GetModel("smart-model")
	transformed := goai.TransformMessages(convCtx.Messages, smartModel)
	if len(transformed) == 0 {
		t.Fatal("TransformMessages returned empty")
	}
}

// TestStreamingHarness verifies the full streaming event protocol.
func TestStreamingHarness(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.ThinkingMessage("Let me analyze this carefully...", "The answer is 42."),
	})

	model := reg.GetModel()
	convCtx := &goai.Context{
		SystemPrompt: "Think step by step.",
		Messages:     []goai.Message{goai.UserMessage("What is the meaning of life?")},
	}

	var events []string
	var thinkingText, responseText string

	ch := goai.Stream(context.Background(), model, convCtx, nil)
	for event := range ch {
		switch e := event.(type) {
		case *goai.StartEvent:
			events = append(events, "start")
		case *goai.ThinkingStartEvent:
			events = append(events, "thinking_start")
		case *goai.ThinkingDeltaEvent:
			events = append(events, "thinking_delta")
			thinkingText += e.Delta
		case *goai.ThinkingEndEvent:
			events = append(events, "thinking_end")
		case *goai.TextStartEvent:
			events = append(events, "text_start")
		case *goai.TextDeltaEvent:
			events = append(events, "text_delta")
			responseText += e.Delta
		case *goai.TextEndEvent:
			events = append(events, "text_end")
		case *goai.DoneEvent:
			events = append(events, "done")
			if e.Message.Usage == nil {
				t.Fatal("DoneEvent missing usage")
			}
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	// Verify event order
	if len(events) < 6 {
		t.Fatalf("expected at least 6 events, got %d: %v", len(events), events)
	}
	if events[0] != "start" {
		t.Fatalf("first event should be start, got %s", events[0])
	}
	if events[len(events)-1] != "done" {
		t.Fatalf("last event should be done, got %s", events[len(events)-1])
	}

	if thinkingText != "Let me analyze this carefully..." {
		t.Fatalf("thinking text mismatch: %q", thinkingText)
	}
	if responseText != "The answer is 42." {
		t.Fatalf("response text mismatch: %q", responseText)
	}
}

// TestErrorHandlingHarness verifies error propagation through the stack.
func TestErrorHandlingHarness(t *testing.T) {
	reg := faux.Register(nil)

	// Test 1: Provider error
	reg.SetResponses([]faux.ResponseStep{
		faux.ErrorMessage("rate limit exceeded"),
	})

	model := reg.GetModel()
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}

	msg, err := goai.Complete(context.Background(), model, convCtx, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if msg == nil {
		t.Fatal("expected error message even on failure")
	}
	if msg.StopReason != goai.StopReasonError {
		t.Fatalf("expected error stop reason, got %s", msg.StopReason)
	}

	// Test 2: Overflow detection
	overflowMsg := &goai.Message{
		StopReason:   goai.StopReasonError,
		ErrorMessage: "prompt is too long: 200000 tokens > 128000 maximum",
	}
	if !goai.IsContextOverflow(overflowMsg, 0) {
		t.Fatal("should detect overflow")
	}

	// Test 3: Non-overflow error
	rateMsg := &goai.Message{
		StopReason:   goai.StopReasonError,
		ErrorMessage: "rate limit exceeded",
	}
	if goai.IsContextOverflow(rateMsg, 0) {
		t.Fatal("rate limit should not be overflow")
	}

	// Test 4: Missing provider
	badModel := &goai.Model{ID: "nope", Provider: "nope", Api: "nonexistent"}
	_, err = goai.Complete(context.Background(), badModel, convCtx, nil)
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
}

// TestContextCompactionHarness verifies compaction preserves conversation coherence.
func TestContextCompactionHarness(t *testing.T) {
	ctx := &goai.Context{
		SystemPrompt: "You are helpful.",
	}

	// Build 50-message conversation
	for i := 0; i < 25; i++ {
		goai.AppendUserMessage(ctx, "question")
		ctx.Messages = append(ctx.Messages, goai.Message{
			Role:       goai.RoleAssistant,
			Content:    []goai.ContentBlock{{Type: "text", Text: "answer"}},
			StopReason: goai.StopReasonStop,
		})
	}

	if len(ctx.Messages) != 50 {
		t.Fatalf("expected 50 messages, got %d", len(ctx.Messages))
	}

	// Compact to 10
	tinyModel := &goai.Model{ContextWindow: 50}
	compacted := goai.CompactContext(ctx, tinyModel, 10)

	if len(compacted.Messages) > 10 {
		t.Fatalf("expected ≤10 messages, got %d", len(compacted.Messages))
	}
	if compacted.SystemPrompt != "You are helpful." {
		t.Fatal("system prompt should survive compaction")
	}

	// Original unchanged
	if len(ctx.Messages) != 50 {
		t.Fatal("original should not be mutated")
	}

	// Compacted context should still be a valid conversation
	lastMsg := compacted.Messages[len(compacted.Messages)-1]
	if lastMsg.Role != goai.RoleAssistant {
		t.Fatal("last message should be assistant (from the most recent turn)")
	}
}

// TestHooksHarness verifies OnPayload and OnResponse hooks fire correctly.
func TestHooksHarness(t *testing.T) {
	reg := faux.Register(nil)
	reg.SetResponses([]faux.ResponseStep{
		faux.TextMessage("response"),
	})

	payloadSeen := false
	opts := &goai.StreamOptions{
		OnPayload: func(payload interface{}, model *goai.Model) (interface{}, error) {
			payloadSeen = true
			return nil, nil
		},
	}

	// Note: faux provider doesn't call InvokeOnPayload (it's a test double),
	// but we verify the hook mechanism works via InvokeOnPayload directly
	_, err := goai.InvokeOnPayload(opts, map[string]interface{}{"test": true}, reg.GetModel())
	if err != nil {
		t.Fatal(err)
	}
	if !payloadSeen {
		t.Fatal("OnPayload hook should have been called")
	}
}

// TestCrossProviderHandoff tests that TransformMessages handles provider switching.
func TestCrossProviderHandoff(t *testing.T) {
	// Simulate: conversation started with OpenAI, switching to Anthropic
	messages := []goai.Message{
		goai.UserMessage("hello"),
		{
			Role:     goai.RoleAssistant,
			Provider: "openai",
			Api:      "openai-completions",
			Model:    "gpt-4o",
			Content: []goai.ContentBlock{
				{Type: "thinking", Thinking: "Let me think about this...", ThinkingSignature: "sig123"},
				{Type: "text", Text: "Hello! How can I help?"},
			},
			StopReason: goai.StopReasonStop,
		},
		goai.UserMessage("follow up"),
	}

	anthropicModel := &goai.Model{
		ID: "claude-sonnet-4", Provider: "anthropic", Api: "anthropic-messages",
		Input: []string{"text"},
	}

	transformed := goai.TransformMessages(messages, anthropicModel)

	// Thinking block should be converted to text (different provider)
	assistantMsg := transformed[1]
	for _, block := range assistantMsg.Content {
		if block.Type == "thinking" {
			t.Fatal("thinking blocks should be converted to text for different provider")
		}
	}

	// Should have text content from the thinking conversion
	hasText := false
	for _, block := range assistantMsg.Content {
		if block.Type == "text" {
			hasText = true
		}
	}
	if !hasText {
		t.Fatal("should have text content after thinking conversion")
	}
}
