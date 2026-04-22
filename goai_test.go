package goai_test

import (
	"encoding/json"
	"os"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUserMessage(t *testing.T) {
	msg := goai.UserMessage("hello")
	if msg.Role != goai.RoleUser {
		t.Fatalf("expected role %q, got %q", goai.RoleUser, msg.Role)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "hello" {
		t.Fatal("unexpected content")
	}
}

func TestContextJSON(t *testing.T) {
	ctx := &goai.Context{
		SystemPrompt: "You are helpful.",
		Messages: []goai.Message{
			goai.UserMessage("hi"),
		},
		Tools: []goai.Tool{{
			Name:        "get_time",
			Description: "Get current time",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		}},
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var decoded goai.Context
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.SystemPrompt != ctx.SystemPrompt {
		t.Fatal("system prompt mismatch")
	}
	if len(decoded.Messages) != 1 || decoded.Messages[0].Content[0].Text != "hi" {
		t.Fatal("message mismatch")
	}
	if len(decoded.Tools) != 1 || decoded.Tools[0].Name != "get_time" {
		t.Fatal("tool mismatch")
	}
}

func TestModelRegistry(t *testing.T) {
	m := &goai.Model{
		ID:       "test-model-2",
		Provider: "test-provider-2",
		Api:      goai.ApiOpenAICompletions,
	}
	goai.RegisterModel(m)
	got := goai.GetModel("test-provider-2", "test-model-2")
	if got == nil {
		t.Fatal("model not found after registration")
	}
	if got.ID != "test-model-2" {
		t.Fatalf("expected ID %q, got %q", "test-model-2", got.ID)
	}
}

func TestStreamNoProvider(t *testing.T) {
	m := &goai.Model{
		ID:       "orphan",
		Provider: "none",
		Api:      "nonexistent-api",
	}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}
	events := goai.Stream(nil, m, ctx, nil)
	for e := range events {
		if _, ok := e.(*goai.ErrorEvent); !ok {
			t.Fatal("expected ErrorEvent for missing provider")
		}
	}
}

// --- Overflow ---

func TestIsContextOverflow(t *testing.T) {
	tests := []struct {
		name    string
		msg     goai.Message
		ctxWin  int
		want    bool
	}{
		{
			name:   "Anthropic overflow",
			msg:    goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "prompt is too long: 213462 tokens > 200000 maximum"},
			want:   true,
		},
		{
			name:   "OpenAI overflow",
			msg:    goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "Your input exceeds the context window"},
			want:   true,
		},
		{
			name:   "rate limit (not overflow)",
			msg:    goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "rate limit exceeded, too many tokens"},
			want:   false,
		},
		{
			name:   "throttling (not overflow)",
			msg:    goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "Throttling error: Too many tokens, please wait"},
			want:   false,
		},
		{
			name:   "silent overflow",
			msg:    goai.Message{StopReason: goai.StopReasonStop, Usage: &goai.Usage{Input: 200000}},
			ctxWin: 128000,
			want:   true,
		},
		{
			name:   "normal response",
			msg:    goai.Message{StopReason: goai.StopReasonStop, Usage: &goai.Usage{Input: 1000}},
			ctxWin: 128000,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := goai.IsContextOverflow(&tt.msg, tt.ctxWin)
			if got != tt.want {
				t.Errorf("IsContextOverflow() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Validation ---

func TestValidateToolCall(t *testing.T) {
	tools := []goai.Tool{{
		Name:        "search",
		Description: "Search the web",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
	}}

	// Valid call
	tc := goai.ToolCall{Name: "search", Arguments: map[string]interface{}{"query": "hello"}}
	_, err := goai.ValidateToolCall(tools, tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Missing required field
	tc2 := goai.ToolCall{Name: "search", Arguments: map[string]interface{}{}}
	_, err = goai.ValidateToolCall(tools, tc2)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}

	// Unknown tool
	tc3 := goai.ToolCall{Name: "nonexistent", Arguments: map[string]interface{}{}}
	_, err = goai.ValidateToolCall(tools, tc3)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// --- Env ---

func TestGetEnvAPIKey(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "test-key-123")
	defer os.Unsetenv("OPENAI_API_KEY")

	key := goai.GetEnvAPIKey(goai.ProviderOpenAI)
	if key != "test-key-123" {
		t.Fatalf("expected 'test-key-123', got %q", key)
	}
}

func TestGetEnvAPIKeyAnthropic(t *testing.T) {
	// ANTHROPIC_OAUTH_TOKEN takes precedence
	os.Setenv("ANTHROPIC_OAUTH_TOKEN", "oauth-token")
	os.Setenv("ANTHROPIC_API_KEY", "api-key")
	defer os.Unsetenv("ANTHROPIC_OAUTH_TOKEN")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	key := goai.GetEnvAPIKey(goai.ProviderAnthropic)
	if key != "oauth-token" {
		t.Fatalf("expected 'oauth-token', got %q", key)
	}
}

// --- Simple options ---

func TestCalculateCost(t *testing.T) {
	model := &goai.Model{
		Cost: goai.ModelCost{Input: 3.0, Output: 15.0, CacheRead: 0.3, CacheWrite: 3.75},
	}
	usage := &goai.Usage{Input: 1000, Output: 500, CacheRead: 200, CacheWrite: 100}
	cost := goai.CalculateCost(model, usage)
	// Input: 1000 * 3.0 / 1M = 0.003
	if cost.Input < 0.002999 || cost.Input > 0.003001 {
		t.Fatalf("unexpected input cost: %f", cost.Input)
	}
	if cost.Total <= 0 {
		t.Fatal("total cost should be > 0")
	}
}

func TestModelsAreEqual(t *testing.T) {
	a := &goai.Model{ID: "gpt-4o", Provider: "openai"}
	b := &goai.Model{ID: "gpt-4o", Provider: "openai"}
	c := &goai.Model{ID: "gpt-4o", Provider: "azure"}

	if !goai.ModelsAreEqual(a, b) {
		t.Fatal("expected equal")
	}
	if goai.ModelsAreEqual(a, c) {
		t.Fatal("expected not equal")
	}
	if goai.ModelsAreEqual(a, nil) {
		t.Fatal("expected not equal with nil")
	}
}

func TestAdjustMaxTokensForThinking(t *testing.T) {
	maxTokens, budget := goai.AdjustMaxTokensForThinking(8192, 200000, goai.ThinkingHigh, nil)
	if budget != 16384 {
		t.Fatalf("expected budget 16384, got %d", budget)
	}
	if maxTokens != 8192+16384 {
		t.Fatalf("expected maxTokens %d, got %d", 8192+16384, maxTokens)
	}
}

// --- Transform ---

func TestTransformSkipsErroredMessages(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: "test", Api: "test", Input: []string{"text"}}
	messages := []goai.Message{
		goai.UserMessage("hi"),
		{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "failed", Content: []goai.ContentBlock{{Type: "text", Text: "partial"}}},
		goai.UserMessage("retry"),
	}
	result := goai.TransformMessages(messages, model)
	// The errored assistant message should be skipped
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
}

func TestTransformDowngradesImages(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: "test", Api: "test", Input: []string{"text"}} // no "image"
	messages := []goai.Message{
		{Role: goai.RoleUser, Content: []goai.ContentBlock{
			{Type: "text", Text: "Look at this:"},
			{Type: "image", Data: "base64...", MimeType: "image/png"},
		}},
	}
	result := goai.TransformMessages(messages, model)
	if len(result[0].Content) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(result[0].Content))
	}
	if result[0].Content[1].Type != "text" {
		t.Fatal("expected image to be replaced with text placeholder")
	}
}

// --- Sanitize ---

func TestSanitizeSurrogates(t *testing.T) {
	// Valid emoji should be preserved
	result := goai.SanitizeSurrogates("Hello 🙈 World")
	if result != "Hello 🙈 World" {
		t.Fatalf("emoji corrupted: %q", result)
	}

	// Normal text
	result = goai.SanitizeSurrogates("plain text")
	if result != "plain text" {
		t.Fatalf("plain text corrupted: %q", result)
	}
}

// --- Compat ---

func TestDetectCompat(t *testing.T) {
	c := goai.DetectCompat("https://api.openai.com/v1")
	if c.SupportsDeveloperRole == nil || !*c.SupportsDeveloperRole {
		t.Fatal("expected SupportsDeveloperRole=true for OpenAI")
	}
	if c.MaxTokensField != "max_completion_tokens" {
		t.Fatalf("expected max_completion_tokens for OpenAI, got %q", c.MaxTokensField)
	}

	c2 := goai.DetectCompat("http://localhost:11434/v1")
	if c2.SupportsStrictMode == nil || *c2.SupportsStrictMode {
		t.Fatal("expected SupportsStrictMode=false for Ollama")
	}
}
