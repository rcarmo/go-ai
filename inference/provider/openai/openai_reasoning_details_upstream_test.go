package openai

import (
	"io"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsReasoningDetailsPreservesBeforeMatchingToolCall(t *testing.T) {
	body := io.NopCloser(io.MultiReader(
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.encrypted\",\"id\":\"call_1\",\"data\":\"encrypted-signature\"}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"tool_calls\",\"delta\":{}}]}\n\n"),
		stringsReader("data: [DONE]\n\n"),
	))
	ch := make(chan goai.Event, 16)
	processSSEStream(body, &goai.Model{ID: "google/gemini-test", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions}, ch)
	close(ch)

	var assistant *goai.Message
	for ev := range ch {
		if d, ok := ev.(*goai.DoneEvent); ok {
			assistant = d.Message
		}
	}
	if assistant == nil {
		t.Fatal("missing assistant message")
	}
	if len(assistant.Content) != 1 || assistant.Content[0].Type != "toolCall" {
		t.Fatalf("assistant content = %#v", assistant.Content)
	}
	wantSig := `{"type":"reasoning.encrypted","id":"call_1","data":"encrypted-signature"}`
	if assistant.Content[0].ThoughtSignature != wantSig {
		t.Fatalf("thought signature = %q, want %q", assistant.Content[0].ThoughtSignature, wantSig)
	}

	req := buildRequestBody(&goai.Model{ID: "google/gemini-test", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions}, &goai.Context{
		Messages: []goai.Message{*assistant},
		Tools:    []goai.Tool{{Name: "read", Description: "Read a file"}},
	}, &goai.StreamOptions{})
	if len(req.Messages) < 1 || len(req.Messages[0].ReasoningDetails) != 1 {
		t.Fatalf("reasoning_details not replayed in assistant payload: %#v", req.Messages)
	}
	if got := req.Messages[0].ReasoningDetails[0]; got.Type != "reasoning.encrypted" || got.ID != "call_1" || got.Data != "encrypted-signature" {
		t.Fatalf("reasoning detail = %#v", got)
	}
}
