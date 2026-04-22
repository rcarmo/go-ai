package goai_test

import (
	"encoding/json"
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
		ID:       "test-model",
		Provider: "test-provider",
		Api:      goai.ApiOpenAICompletions,
	}
	goai.RegisterModel(m)
	got := goai.GetModel("test-provider", "test-model")
	if got == nil {
		t.Fatal("model not found after registration")
	}
	if got.ID != "test-model" {
		t.Fatalf("expected ID %q, got %q", "test-model", got.ID)
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
