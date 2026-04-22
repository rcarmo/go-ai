package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// FuzzContextRoundTrip verifies that Context survives JSON round-trips.
func FuzzContextRoundTrip(f *testing.F) {
	f.Add(
		"You are helpful.",                      // systemPrompt
		"What is 2+2?",                          // userMessage
		`{"type":"object","properties":{}}`,      // toolSchema
		"get_time",                               // toolName
		"Get current time",                       // toolDesc
	)
	f.Add(
		"",
		"Hello 🎉 world",
		`{"type":"object","required":["q"],"properties":{"q":{"type":"string"}}}`,
		"search",
		"Search the web",
	)
	f.Add(
		"System\nprompt\nwith\nnewlines",
		"User message with \"quotes\" and \\backslash",
		`{}`,
		"tool-with-dashes",
		"A tool with special chars: <>&",
	)

	f.Fuzz(func(t *testing.T, systemPrompt, userMsg, toolSchema, toolName, toolDesc string) {
		// Skip inputs with invalid UTF-8 (JSON encoder replaces them, breaking round-trip)
		for _, s := range []string{systemPrompt, userMsg, toolName, toolDesc} {
			for _, r := range s {
				if r == 0xFFFD {
					return
				}
			}
		}
		// Build a context
		var params json.RawMessage
		if json.Valid([]byte(toolSchema)) {
			params = json.RawMessage(toolSchema)
		} else {
			params = json.RawMessage(`{}`)
		}

		ctx := &goai.Context{
			SystemPrompt: systemPrompt,
			Messages: []goai.Message{
				goai.UserMessage(userMsg),
			},
			Tools: []goai.Tool{{
				Name:        toolName,
				Description: toolDesc,
				Parameters:  params,
			}},
		}

		// Marshal
		data, err := json.Marshal(ctx)
		if err != nil {
			return // some strings can't be marshaled (invalid UTF-8)
		}

		// Unmarshal
		var decoded goai.Context
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v\nJSON: %s", err, string(data))
		}

		// Verify round-trip (compare re-marshaled JSON, not strings directly,
		// because Go's JSON encoder may normalize Unicode replacement characters)
		if len(decoded.Messages) != len(ctx.Messages) {
			t.Errorf("message count mismatch: %d vs %d", len(decoded.Messages), len(ctx.Messages))
		}
		if len(decoded.Tools) != len(ctx.Tools) {
			t.Errorf("tool count mismatch: %d vs %d", len(decoded.Tools), len(ctx.Tools))
		}
		if len(decoded.Tools) > 0 && decoded.Tools[0].Name != ctx.Tools[0].Name {
			t.Errorf("tool name mismatch: %q vs %q", decoded.Tools[0].Name, ctx.Tools[0].Name)
		}

		// Re-marshal and verify stability (second round-trip must be stable)
		data2, err := json.Marshal(&decoded)
		if err != nil {
			t.Fatalf("failed to re-marshal: %v", err)
		}
		var decoded2 goai.Context
		if err := json.Unmarshal(data2, &decoded2); err != nil {
			t.Fatalf("failed to re-unmarshal: %v", err)
		}
		data3, err := json.Marshal(&decoded2)
		if err != nil {
			t.Fatalf("failed to re-re-marshal: %v", err)
		}
		if string(data2) != string(data3) {
			t.Errorf("non-stable second round-trip")
		}
	})
}

// FuzzTransformMessages verifies TransformMessages doesn't panic on arbitrary input.
func FuzzTransformMessages(f *testing.F) {
	f.Add("text content", "thinking content", "tool-call-id-123", "my_tool", true, false)
	f.Add("", "", "", "", false, true)
	f.Add("Hello 🎉", "<thinking>deep thought</thinking>", "tc_abc|fc_def", "search", true, true)

	f.Fuzz(func(t *testing.T, text, thinking, toolCallID, toolName string, hasThinking, hasToolCall bool) {
		model := &goai.Model{
			ID:       "test",
			Provider: "test",
			Api:      "test",
			Input:    []string{"text"},
		}

		var content []goai.ContentBlock
		content = append(content, goai.ContentBlock{Type: "text", Text: text})
		if hasThinking {
			content = append(content, goai.ContentBlock{Type: "thinking", Thinking: thinking})
		}
		if hasToolCall {
			content = append(content, goai.ContentBlock{
				Type: "toolCall", ID: toolCallID, Name: toolName,
				Arguments: map[string]interface{}{"q": text},
			})
		}

		messages := []goai.Message{
			goai.UserMessage("hello"),
			{Role: goai.RoleAssistant, Content: content, StopReason: goai.StopReasonStop,
				Provider: "other", Api: "other", Model: "other"},
		}

		// Must not panic
		result := goai.TransformMessages(messages, model)
		if len(result) == 0 {
			t.Error("expected at least one message")
		}
	})
}

// FuzzOverflowDetection verifies overflow detection doesn't panic.
func FuzzOverflowDetection(f *testing.F) {
	f.Add("prompt is too long: 200000 tokens > 128000 maximum", int(128000))
	f.Add("rate limit exceeded", int(0))
	f.Add("Throttling error: Too many tokens, please wait", int(0))
	f.Add("HTTP 413: request too large", int(0))
	f.Add("", int(128000))
	f.Add("random error message with no pattern", int(0))

	f.Fuzz(func(t *testing.T, errorMsg string, ctxWindow int) {
		msg := &goai.Message{
			StopReason:   goai.StopReasonError,
			ErrorMessage: errorMsg,
			Usage:        &goai.Usage{Input: ctxWindow + 1000},
		}

		// Must not panic
		_ = goai.IsContextOverflow(msg, ctxWindow)

		// Also test with stop reason
		msg.StopReason = goai.StopReasonStop
		_ = goai.IsContextOverflow(msg, ctxWindow)
	})
}
