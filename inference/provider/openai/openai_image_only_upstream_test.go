package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0871OpenAICompletionsOmitsEmptyTextPartsFromImageOnlyUserMessages(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1", Input: []string{"text", "image"}}
	ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: ""}, {Type: "image", Data: "ZmFrZQ==", MimeType: "image/png"}}}}}
	req := buildRequestBody(model, ctx, nil)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text,omitempty"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url,omitempty"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Messages) != 1 || payload.Messages[0].Role != "user" {
		t.Fatalf("messages=%#v payload=%s", payload.Messages, data)
	}
	content := payload.Messages[0].Content
	if len(content) != 1 || content[0].Type != "image_url" || content[0].ImageURL == nil || content[0].ImageURL.URL != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("content=%#v payload=%s", content, data)
	}
}

func TestV0871OpenAICompletionsPreservesWhitespaceTextPartsWithImages(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1", Input: []string{"text", "image"}}
	ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: " "}, {Type: "image", Data: "ZmFrZQ==", MimeType: "image/png"}}}}}
	req := buildRequestBody(model, ctx, nil)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Messages) != 1 || len(payload.Messages[0].Content) != 2 || payload.Messages[0].Content[0].Type != "text" || payload.Messages[0].Content[0].Text != " " {
		t.Fatalf("whitespace text part was not preserved: %#v payload=%s", payload.Messages, data)
	}
}
