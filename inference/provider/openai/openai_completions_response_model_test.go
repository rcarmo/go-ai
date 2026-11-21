package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	processSSEStream(body, model, goai.NewAssistantEventSender(ch))
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

// Source: openai-completions-tool-choice.test.ts, Copilot's top-level usage.
func TestSourceV160OpenAIReasoningUsageFallback(t *testing.T) {
	goai.RegisterBuiltinModels()
	registered := goai.GetModel("github-copilot", "gemini-4-argon")
	if registered == nil {
		t.Fatal("missing Copilot model")
	}
	for _, tc := range []struct {
		details   string
		reasoning int
	}{{"", 30}, {`,"completion_tokens_details":{"reasoning_tokens":0}`, 30}, {`,"completion_tokens_details":{"reasoning_tokens":7}`, 7}} {
		chunks := []string{
			`data: {"id":"chatcmpl-copilot-reasoning","choices":[{"index":0,"delta":{"content":"OK"}}]}`,
			`data: {"id":"chatcmpl-copilot-reasoning","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":40,"prompt_tokens_details":{"cached_tokens":0},"reasoning_tokens":30` + tc.details + `}}`,
			`data: [DONE]`,
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, strings.Join(chunks, "\n\n")+"\n\n")
		}))
		model := *registered
		model.BaseURL = server.URL
		var message *goai.Message
		for event := range streamOpenAISimple(context.Background(), &model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Reply with exactly OK")}}, &goai.StreamOptions{APIKey: "test"}) {
			if done, ok := event.(*goai.DoneEvent); ok {
				message = done.Message
			}
			if failed, ok := event.(*goai.ErrorEvent); ok {
				t.Errorf("Copilot stream failed: %v", failed.Err)
			}
		}
		server.Close()
		if message == nil {
			t.Fatal("Copilot stream had no result")
		}
		if message.Usage == nil || !message.Usage.ReasoningSet || message.Usage.Reasoning != tc.reasoning || message.Usage.Output != 40 {
			t.Errorf("details=%q, usage=%+v; want reasoning=%d, output=40", tc.details, message.Usage, tc.reasoning)
		}
	}
}
