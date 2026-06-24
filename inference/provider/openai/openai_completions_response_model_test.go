package openai

import (
	"io"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsResponseModelSurfacesRoutedChunkModelWithoutChangingModel(t *testing.T) {
	message := runOpenAICompletionsResponseModelChunks(t, []string{
		`data: {"id":"chatcmpl-1","model":"anthropic/claude-opus-4.8","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`data: {"id":"chatcmpl-1","model":"anthropic/claude-opus-4.8","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	})
	if message.Model != "openrouter/auto" {
		t.Fatalf("model=%q, want openrouter/auto", message.Model)
	}
	if message.ResponseModel != "anthropic/claude-opus-4.8" {
		t.Fatalf("responseModel=%q, want anthropic/claude-opus-4.8", message.ResponseModel)
	}
	if message.Provider != goai.ProviderOpenRouter {
		t.Fatalf("provider=%q, want openrouter", message.Provider)
	}
	if message.StopReason != goai.StopReasonStop {
		t.Fatalf("stopReason=%q, want stop", message.StopReason)
	}
}

func TestOpenAICompletionsResponseModelLeavesUndefinedWhenChunksEchoRequestedID(t *testing.T) {
	message := runOpenAICompletionsResponseModelChunks(t, []string{
		`data: {"id":"chatcmpl-2","model":"openrouter/auto","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`data: {"id":"chatcmpl-2","model":"openrouter/auto","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	})
	if message.Model != "openrouter/auto" {
		t.Fatalf("model=%q, want openrouter/auto", message.Model)
	}
	if message.ResponseModel != "" {
		t.Fatalf("responseModel=%q, want empty", message.ResponseModel)
	}
}

func TestOpenAICompletionsResponseModelIgnoresEmptyOrMissingChunkModel(t *testing.T) {
	message := runOpenAICompletionsResponseModelChunks(t, []string{
		`data: {"id":"chatcmpl-3","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`data: {"id":"chatcmpl-3","model":"","choices":[{"index":0,"delta":{"content":"!"}}]}`,
		`data: {"id":"chatcmpl-3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
	})
	if message.Model != "openrouter/auto" {
		t.Fatalf("model=%q, want openrouter/auto", message.Model)
	}
	if message.ResponseModel != "" {
		t.Fatalf("responseModel=%q, want empty", message.ResponseModel)
	}
}

func runOpenAICompletionsResponseModelChunks(t *testing.T, chunks []string) *goai.Message {
	t.Helper()
	var b strings.Builder
	for _, chunk := range chunks {
		b.WriteString(chunk)
		b.WriteString("\n\n")
	}
	body := io.NopCloser(strings.NewReader(b.String()))
	ch := make(chan goai.Event, 16)
	model := &goai.Model{ID: "openrouter/auto", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions}
	processSSEStream(body, model, ch)
	close(ch)
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			return e.Message
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	t.Fatal("expected DoneEvent")
	return nil
}
