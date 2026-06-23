package goai_test

import (
	"context"
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
	events := goai.Stream(context.Background(), m, ctx, nil)
	for e := range events {
		if _, ok := e.(*goai.ErrorEvent); !ok {
			t.Fatal("expected ErrorEvent for missing provider")
		}
	}
}

// --- Overflow ---

func TestIsContextOverflow(t *testing.T) {
	tests := []struct {
		name   string
		msg    goai.Message
		ctxWin int
		want   bool
	}{
		{
			name: "Anthropic overflow",
			msg:  goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "prompt is too long: 213462 tokens > 200000 maximum"},
			want: true,
		},
		{
			name: "OpenAI overflow",
			msg:  goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "Your input exceeds the context window"},
			want: true,
		},
		{
			name: "rate limit (not overflow)",
			msg:  goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "rate limit exceeded, too many tokens"},
			want: false,
		},
		{
			name: "throttling (not overflow)",
			msg:  goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "Throttling error: Too many tokens, please wait"},
			want: false,
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
		{
			name: "OpenAI/LiteLLM max context length",
			msg:  goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "Requested token count exceeds the model's maximum context length of 131072 tokens"},
			want: true,
		},
		{
			name: "OpenRouter/Poolside max allowed input",
			msg:  goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "Input length 265330 exceeds the maximum allowed input length of 262144 tokens."},
			want: true,
		},
		{
			name: "Together AI context length",
			msg:  goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "The input (200000 tokens) is longer than the model's context length (131072 tokens)."},
			want: true,
		},
		{
			name:   "Xiaomi MiMo length-stop overflow",
			msg:    goai.Message{StopReason: goai.StopReasonLength, Usage: &goai.Usage{Input: 131000, Output: 0}},
			ctxWin: 131072,
			want:   true,
		},
		{
			name:   "length stop with normal output (not overflow)",
			msg:    goai.Message{StopReason: goai.StopReasonLength, Usage: &goai.Usage{Input: 100000, Output: 4096}},
			ctxWin: 131072,
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

func TestGetEnvAPIKeyWithEnvBedrockAuthenticated(t *testing.T) {
	key := goai.GetEnvAPIKeyWithEnv(goai.ProviderAmazonBedrock, goai.ProviderEnv{
		"AWS_ACCESS_KEY_ID":     "akid",
		"AWS_SECRET_ACCESS_KEY": "secret",
	})
	if key != "<authenticated>" {
		t.Fatalf("expected authenticated marker, got %q", key)
	}
	key = goai.GetEnvAPIKeyWithEnv(goai.ProviderAmazonBedrock, goai.ProviderEnv{"AWS_BEARER_TOKEN_BEDROCK": "token"})
	if key != "<authenticated>" {
		t.Fatalf("expected bearer token authenticated marker, got %q", key)
	}
}

func TestGetEnvAPIKeyWithEnvGoogleVertexADC(t *testing.T) {
	dir := t.TempDir()
	credentialsPath := dir + "/adc.json"
	if err := os.WriteFile(credentialsPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	key := goai.GetEnvAPIKeyWithEnv(goai.ProviderGoogleVertex, goai.ProviderEnv{
		"GOOGLE_APPLICATION_CREDENTIALS": credentialsPath,
		"GOOGLE_CLOUD_PROJECT":           "project",
		"GOOGLE_CLOUD_LOCATION":          "us-central1",
	})
	if key != "<authenticated>" {
		t.Fatalf("expected ADC authenticated marker, got %q", key)
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

func TestCalculateCostAnthropicLongCacheWrite(t *testing.T) {
	model := &goai.Model{Cost: goai.ModelCost{Input: 3.0, Output: 15.0, CacheRead: 0.3, CacheWrite: 3.75}}
	usage := &goai.Usage{CacheWrite: 1000, CacheWrite1h: 250}
	cost := goai.CalculateCost(model, usage)
	// 750 short cache writes at 3.75/M + 250 1h writes at 2x input (6.0/M)
	want := (750*3.75 + 250*6.0) / 1_000_000
	if cost.CacheWrite < want-0.0000001 || cost.CacheWrite > want+0.0000001 {
		t.Fatalf("unexpected long cache write cost: got %f want %f", cost.CacheWrite, want)
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
	remotePort := goai.DetectCompat("https://example.com:11434/v1")
	if remotePort.MaxTokensField != "max_completion_tokens" {
		t.Fatalf("expected remote :11434 URL not to be treated as Ollama, got %+v", remotePort)
	}
	pathMention := goai.DetectCompat("https://proxy.example.com/v1/localhost:11434/api")
	if pathMention.MaxTokensField != "max_completion_tokens" {
		t.Fatalf("expected localhost:11434 in path not to be treated as Ollama, got %+v", pathMention)
	}

	moonshot := goai.DetectCompatForModel(&goai.Model{Provider: goai.ProviderMoonshotAI, BaseURL: "https://api.moonshot.ai/v1"})
	if moonshot.MaxTokensField != "max_tokens" || moonshot.SupportsReasoningEffort == nil || *moonshot.SupportsReasoningEffort || moonshot.SupportsStrictMode == nil || *moonshot.SupportsStrictMode {
		t.Fatalf("unexpected Moonshot compat: %+v", moonshot)
	}

	cf := goai.DetectCompatForModel(&goai.Model{Provider: goai.ProviderCloudflareAIGateway, BaseURL: "https://gateway.ai.cloudflare.com/v1/a/b/compat"})
	if cf.MaxTokensField != "max_tokens" || cf.SupportsLongCacheRetention == nil || *cf.SupportsLongCacheRetention {
		t.Fatalf("unexpected Cloudflare AI Gateway compat: %+v", cf)
	}

	xiaomi := goai.DetectCompatForModel(&goai.Model{Provider: goai.ProviderXiaomi, BaseURL: "https://api.xiaomimimo.com/v1"})
	if xiaomi.ThinkingFormat != "deepseek" || xiaomi.RequiresReasoningContentOnAssistantMessages == nil || !*xiaomi.RequiresReasoningContentOnAssistantMessages {
		t.Fatalf("unexpected Xiaomi compat: %+v", xiaomi)
	}
}

func TestClampThinkingLevelPrefersUpgrade(t *testing.T) {
	high := "high"
	model := &goai.Model{
		Reasoning: true,
		ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{
			goai.ThinkingOff: nil,
			goai.ModelThinkingLevel(goai.ThinkingLow):    nil,
			goai.ModelThinkingLevel(goai.ThinkingMedium): nil,
			goai.ModelThinkingLevel(goai.ThinkingHigh):   &high,
		},
	}
	// Upstream prefers upgrading to next available level
	if got := goai.ClampThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingMedium)); got != goai.ModelThinkingLevel(goai.ThinkingHigh) {
		t.Fatalf("expected unsupported medium to upgrade to high, got %q", got)
	}
}

func TestHasOpenAIAuthHeader(t *testing.T) {
	if !goai.HasOpenAIAuthHeader(map[string]string{"authorization": "Bearer custom"}) {
		t.Fatal("expected Authorization header to be detected")
	}
	if !goai.HasOpenAIAuthHeader(map[string]string{"CF-AIG-Authorization": "Bearer cf"}) {
		t.Fatal("expected cf-aig-authorization header to be detected")
	}
	if goai.HasOpenAIAuthHeader(map[string]string{"Authorization": "   "}) {
		t.Fatal("blank auth header should not be detected")
	}
}

func TestHasAnthropicAuthHeader(t *testing.T) {
	if !goai.HasAnthropicAuthHeader(map[string]string{"authorization": "Bearer custom"}) {
		t.Fatal("expected Authorization header to be detected")
	}
	if !goai.HasAnthropicAuthHeader(map[string]string{"X-Api-Key": "custom"}) {
		t.Fatal("expected X-Api-Key header to be detected")
	}
	if !goai.HasAnthropicAuthHeader(map[string]string{"cf-aig-authorization": "Bearer cf"}) {
		t.Fatal("expected cf-aig-authorization header to be detected")
	}
	if goai.HasAnthropicAuthHeader(map[string]string{"X-Api-Key": "   "}) {
		t.Fatal("blank auth header should not be detected")
	}
}

func TestBuildCopilotDynamicHeaders(t *testing.T) {
	// Last message from user => user-initiated, no images.
	h := goai.BuildCopilotDynamicHeaders([]goai.Message{
		{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hi"}}},
	})
	if h["X-Initiator"] != "user" {
		t.Fatalf("expected X-Initiator=user, got %q", h["X-Initiator"])
	}
	if h["Openai-Intent"] != "conversation-edits" {
		t.Fatalf("expected Openai-Intent=conversation-edits, got %q", h["Openai-Intent"])
	}
	if _, ok := h["Copilot-Vision-Request"]; ok {
		t.Fatal("did not expect Copilot-Vision-Request without images")
	}

	// Last message from assistant => agent-initiated; user image present => vision.
	h = goai.BuildCopilotDynamicHeaders([]goai.Message{
		{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "image"}}},
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}},
	})
	if h["X-Initiator"] != "agent" {
		t.Fatalf("expected X-Initiator=agent, got %q", h["X-Initiator"])
	}
	if h["Copilot-Vision-Request"] != "true" {
		t.Fatalf("expected Copilot-Vision-Request=true, got %q", h["Copilot-Vision-Request"])
	}
}
