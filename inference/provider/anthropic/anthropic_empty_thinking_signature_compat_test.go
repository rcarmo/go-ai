package anthropic

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicEmptyThinkingSignatureCompatConvertsEmptySignatureThinkingToTextByDefault(t *testing.T) {
	payload := buildRequest(makeEmptySignatureModel(nil), makeEmptySignatureContext(""), &goai.StreamOptions{})
	assistant := firstAssistantContent(t, payload)
	want := []anthropicContentBlock{{Type: "text", Text: "internal reasoning"}}
	if !reflect.DeepEqual(assistant, want) {
		t.Fatalf("assistant content=%#v, want %#v", assistant, want)
	}
}

func TestAnthropicEmptyThinkingSignatureCompatPreservesEmptySignatureWhenAllowed(t *testing.T) {
	payload := buildRequest(makeEmptySignatureModel(boolPtrCompat(true)), makeEmptySignatureContext(" "), &goai.StreamOptions{})
	assistant := firstAssistantContent(t, payload)
	want := []anthropicContentBlock{{Type: "thinking", Thinking: "internal reasoning", Signature: ""}}
	if !reflect.DeepEqual(assistant, want) {
		t.Fatalf("assistant content=%#v, want %#v", assistant, want)
	}
}

func makeEmptySignatureModel(allow *bool) *goai.Model {
	var compat *goai.AnthropicMessagesCompat
	if allow != nil {
		compat = &goai.AnthropicMessagesCompat{AllowEmptySignature: allow}
	}
	return &goai.Model{ID: "mimo-v2.5-pro", Provider: "xiaomi-token-plan-ams", Api: goai.ApiAnthropicMessages, Reasoning: true, AnthropicCompat: compat}
}

func makeEmptySignatureContext(signature string) *goai.Context {
	assistant := goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "internal reasoning", ThinkingSignature: signature}}, Provider: "xiaomi-token-plan-ams", Api: goai.ApiAnthropicMessages, Model: "mimo-v2.5-pro", StopReason: goai.StopReasonStop, Usage: &goai.Usage{}}
	return &goai.Context{Messages: []goai.Message{goai.UserMessage("first"), assistant, goai.UserMessage("second")}}
}

func firstAssistantContent(t *testing.T, req anthropicRequest) []anthropicContentBlock {
	t.Helper()
	for _, msg := range req.Messages {
		if msg.Role == "assistant" {
			content, ok := msg.Content.([]anthropicContentBlock)
			if !ok {
				t.Fatalf("assistant content type = %T", msg.Content)
			}
			return content
		}
	}
	t.Fatal("assistant message not found")
	return nil
}
