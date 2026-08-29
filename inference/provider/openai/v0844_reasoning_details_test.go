package openai

import (
	"encoding/json"
	"io"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0844OpenAICompletionsReasoningDetailsMergeAndReplay(t *testing.T) {
	body := io.NopCloser(io.MultiReader(
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.text\",\"text\":\"The\",\"index\":0}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.text\",\"text\":\" user wants the time.\",\"signature\":\"sha256:text-signature\",\"format\":\"openai-responses-v1\",\"index\":0}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.summary\",\"summary\":\"Looked\",\"index\":0}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.summary\",\"summary\":\" up time.\",\"format\":\"openai-responses-v1\",\"index\":0}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.encrypted\",\"id\":\"call_1\",\"data\":\"encrypted-signature\"}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.summary\",\"summary\":\"After encrypted block.\",\"format\":\"openai-responses-v1\",\"index\":0}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"tool_calls\",\"delta\":{}}]}\n\n"),
		stringsReader("data: [DONE]\n\n"),
	))
	model := &goai.Model{ID: "google/gemini-test", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions}
	ch := make(chan goai.Event, 32)
	processSSEStream(body, model, ch)
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
	if len(assistant.Content) != 2 || assistant.Content[0].Type != "thinking" || assistant.Content[1].Type != "toolCall" {
		t.Fatalf("assistant content = %#v", assistant.Content)
	}
	if assistant.Content[0].Thinking != "" {
		t.Fatalf("reasoning_details should not duplicate visible thinking, got %q", assistant.Content[0].Thinking)
	}
	var details []reasoningDetail
	if err := json.Unmarshal([]byte(assistant.Content[0].ThinkingSignature), &details); err != nil {
		t.Fatalf("invalid thinking signature: %v", err)
	}
	wantSignature := "sha256:text-signature"
	wantID := "call_1"
	wantIndex := 0
	want := []reasoningDetail{
		{Type: "reasoning.text", Text: "The user wants the time.", Signature: &wantSignature, Format: "openai-responses-v1", Index: &wantIndex},
		{Type: "reasoning.summary", Summary: "Looked up time.", Format: "openai-responses-v1", Index: &wantIndex},
		{Type: "reasoning.encrypted", ID: &wantID, Data: "encrypted-signature"},
		{Type: "reasoning.summary", Summary: "After encrypted block.", Format: "openai-responses-v1", Index: &wantIndex},
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("details=%#v want %#v", details, want)
	}
	if assistant.Content[1].ThoughtSignature != "" {
		t.Fatalf("toolCall should not receive duplicate reasoning signature: %#v", assistant.Content[1])
	}

	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{*assistant}, Tools: []goai.Tool{{Name: "read", Description: "Read a file"}}}, &goai.StreamOptions{})
	if len(req.Messages) < 1 || !reflect.DeepEqual(req.Messages[0].ReasoningDetails, want) {
		t.Fatalf("reasoning_details replay=%#v want %#v", req.Messages, want)
	}
}

func TestV0844OpenAICompletionsExplicitToolChoiceNoneSerializesWithoutTools(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{ToolChoice: goai.ToolChoiceNone})
	if req.ToolChoice != "none" {
		t.Fatalf("tool_choice=%q, want none", req.ToolChoice)
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tool_choice"] != "none" {
		t.Fatalf("payload=%s", b)
	}
	if _, ok := payload["tools"]; ok {
		t.Fatalf("tools should be omitted with explicit none and no tools: %s", b)
	}
}
