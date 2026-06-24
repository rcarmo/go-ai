package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsCacheControlFormatAppliesAnthropicStyleMarkers(t *testing.T) {
	model := &goai.Model{
		ID: "custom-qwen", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions, BaseURL: "https://example.com/v1",
		Reasoning:         true,
		CompletionsCompat: &goai.OpenAICompletionsCompat{CacheControlFormat: "anthropic"},
	}
	ctx := &goai.Context{
		SystemPrompt: "System prompt",
		Messages:     []goai.Message{goai.UserMessage("Hello")},
		Tools:        []goai.Tool{{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}},
	}

	req := buildRequestBody(model, ctx, &goai.StreamOptions{})

	assertInstructionCacheMarker(t, req)
	if len(req.Tools) != 1 || req.Tools[0].CacheControl == nil || req.Tools[0].CacheControl.Type != "ephemeral" || req.Tools[0].CacheControl.TTL != "" {
		t.Fatalf("tool cache_control = %#v", req.Tools)
	}
	assertLastUserCacheMarker(t, req)
}

func TestOpenAICompletionsCacheControlFormatPreservesOpenRouterAnthropicMarkers(t *testing.T) {
	model := &goai.Model{
		ID: "anthropic/claude-sonnet-4", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions, BaseURL: "https://openrouter.ai/api/v1",
		Reasoning: true,
	}
	ctx := &goai.Context{
		SystemPrompt: "System prompt",
		Messages:     []goai.Message{goai.UserMessage("Hello")},
		Tools:        []goai.Tool{{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}

	req := buildRequestBody(model, ctx, &goai.StreamOptions{})

	assertInstructionCacheMarker(t, req)
	if len(req.Tools) != 1 || req.Tools[0].CacheControl == nil || req.Tools[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("tool cache_control = %#v", req.Tools)
	}
	assertLastUserCacheMarker(t, req)
}

func TestOpenAICompletionsCacheControlFormatOmitsMarkersWhenRetentionNone(t *testing.T) {
	model := &goai.Model{
		ID: "custom-qwen", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions, BaseURL: "https://example.com/v1",
		Reasoning:         true,
		CompletionsCompat: &goai.OpenAICompletionsCompat{CacheControlFormat: "anthropic"},
	}
	ctx := &goai.Context{
		SystemPrompt: "System prompt",
		Messages:     []goai.Message{goai.UserMessage("Hello")},
		Tools:        []goai.Tool{{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}

	req := buildRequestBody(model, ctx, &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})

	if _, ok := req.Messages[0].Content.(string); !ok {
		t.Fatalf("instruction content should remain plain string when cache retention none: %#v", req.Messages[0].Content)
	}
	if len(req.Tools) != 1 && req.Tools[0].CacheControl != nil {
		t.Fatalf("tool cache_control should be omitted: %#v", req.Tools)
	}
	if _, ok := req.Messages[len(req.Messages)-1].Content.(string); !ok {
		t.Fatalf("last user content should remain plain string when cache retention none: %#v", req.Messages[len(req.Messages)-1].Content)
	}
}

func assertInstructionCacheMarker(t *testing.T, req chatRequest) {
	t.Helper()
	for _, msg := range req.Messages {
		if msg.Role != "system" && msg.Role != "developer" {
			continue
		}
		parts, ok := msg.Content.([]contentPart)
		if !ok || len(parts) == 0 || parts[0].CacheControl == nil || parts[0].CacheControl.Type != "ephemeral" {
			t.Fatalf("instruction cache marker missing: %#v", msg.Content)
		}
		return
	}
	t.Fatal("missing instruction message")
}

func assertLastUserCacheMarker(t *testing.T, req chatRequest) {
	t.Helper()
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "user" {
		t.Fatalf("last message role = %q", last.Role)
	}
	parts, ok := last.Content.([]contentPart)
	if !ok || len(parts) == 0 || parts[0].CacheControl == nil || parts[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("last user cache marker missing: %#v", last.Content)
	}
}
