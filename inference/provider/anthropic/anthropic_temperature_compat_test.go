package anthropic

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicTemperatureCompatOmitsTemperatureForClaudeOpus47(t *testing.T) {
	model := mustAnthropicModel(t, "claude-opus-4-7")
	req := captureAnthropicTemperaturePayload(t, model, 0)
	if req.Temperature != nil {
		t.Fatalf("temperature = %v, want omitted", *req.Temperature)
	}
}

func TestAnthropicTemperatureCompatOmitsTemperatureForClaudeOpus48(t *testing.T) {
	model := mustAnthropicModel(t, "claude-opus-4-8")
	req := captureAnthropicTemperaturePayload(t, model, 0)
	if req.Temperature != nil {
		t.Fatalf("temperature = %v, want omitted", *req.Temperature)
	}
}

func TestAnthropicTemperatureCompatOmitsDefaultTemperatureForClaudeOpus47(t *testing.T) {
	model := mustAnthropicModel(t, "claude-opus-4-7")
	req := captureAnthropicTemperaturePayload(t, model, 1)
	if req.Temperature != nil {
		t.Fatalf("temperature = %v, want omitted", *req.Temperature)
	}
}

func TestAnthropicTemperatureCompatKeepsTemperatureForClaudeOpus46(t *testing.T) {
	model := mustAnthropicModel(t, "claude-opus-4-6")
	req := captureAnthropicTemperaturePayload(t, model, 0)
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0", req.Temperature)
	}
}

func TestAnthropicTemperatureCompatKeepsTemperatureForClaudeSonnet46(t *testing.T) {
	model := mustAnthropicModel(t, "claude-sonnet-4-6")
	req := captureAnthropicTemperaturePayload(t, model, 0)
	if req.Temperature == nil || *req.Temperature != 0 {
		t.Fatalf("temperature = %v, want 0", req.Temperature)
	}
}

func TestAnthropicTemperatureCompatOmitsTemperatureForCustomModelsWithSupportsTemperatureDisabled(t *testing.T) {
	model := &goai.Model{
		ID:              "vendor--claude-opus-4-7",
		Name:            "Vendor Proxy Opus 4.7",
		Api:             goai.ApiAnthropicMessages,
		Provider:        "vendor-proxy",
		BaseURL:         "http://127.0.0.1:9",
		Reasoning:       true,
		Input:           []string{"text"},
		Cost:            goai.ModelCost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:   200000,
		MaxTokens:       32000,
		AnthropicCompat: &goai.AnthropicMessagesCompat{SupportsTemperature: boolPtr(false)},
	}
	req := captureAnthropicTemperaturePayload(t, model, 0)
	if req.Temperature != nil {
		t.Fatalf("temperature = %v, want omitted", *req.Temperature)
	}
}

func mustAnthropicModel(t *testing.T, id string) *goai.Model {
	t.Helper()
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderAnthropic, id)
	if model == nil {
		t.Fatalf("missing anthropic model %q", id)
	}
	return model
}

func captureAnthropicTemperaturePayload(t *testing.T, model *goai.Model, temperature float64) anthropicRequest {
	t.Helper()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}
	return buildRequest(model, ctx, &goai.StreamOptions{Temperature: &temperature, APIKey: "fake-key"})
}
