package openairesponses

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAIResponsesMessageIDGeneratesUniqueFallbackMessageIDsForMultipleTextBlocks(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.5", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAIResponses}
	assistant := goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "private reasoning"}, {Type: "text", Text: "visible answer"}}, Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic, Model: "claude-opus-4-8", Usage: &goai.Usage{}, StopReason: goai.StopReasonStop}
	ctx := &goai.Context{SystemPrompt: "You are concise.", Messages: []goai.Message{goai.UserMessage("hello"), assistant}}
	input := convertMessages(model, ctx)
	var ids []string
	for _, item := range input {
		m, ok := item.(map[string]interface{})
		if !ok || m["type"] != "message" {
			continue
		}
		if id, ok := m["id"].(string); ok {
			ids = append(ids, id)
		}
	}
	want := []string{"msg_pi_1", "msg_pi_1_1"}
	if len(ids) != len(want) {
		t.Fatalf("message ids=%#v, want %#v", ids, want)
	}
	seen := map[string]bool{}
	for i, id := range ids {
		if id != want[i] {
			t.Fatalf("message ids=%#v, want %#v", ids, want)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q in %#v", id, ids)
		}
		seen[id] = true
	}
}
