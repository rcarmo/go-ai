package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAzureOpenAIResponsesReasoningReplayPreservesOutputItemEncryptedContent(t *testing.T) {
	done := runReasoningReplayStream(t, `{"type":"reasoning","id":"rs_done","summary":[],"encrypted_content":"from-output-item-done"}`, `{"type":"reasoning","id":"rs_done","summary":[],"encrypted_content":"from-response-completed"}`)
	got := reasoningSignatureMap(t, done)
	if got["id"] != "rs_done" || got["encrypted_content"] != "from-output-item-done" {
		t.Fatalf("unexpected replay signature: %#v", got)
	}
}

func TestAzureOpenAIResponsesReasoningReplayBackfillsCompletedEncryptedContent(t *testing.T) {
	done := runReasoningReplayStream(t, `{"type":"reasoning","id":"rs_missing","summary":[]}`, `{"type":"reasoning","id":"rs_missing","summary":[],"encrypted_content":"from-response-completed"}`)
	got := reasoningSignatureMap(t, done)
	if got["id"] != "rs_missing" || got["encrypted_content"] != "from-response-completed" {
		t.Fatalf("unexpected replay signature: %#v", got)
	}
}

func runReasoningReplayStream(t *testing.T, doneItem, completedItem string) *goai.DoneEvent {
	t.Helper()
	body := strings.NewReader(strings.Join([]string{
		`data: {"type":"response.output_item.added","output_index":0,"sequence_number":0,"item":{"type":"reasoning","id":"rs"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"sequence_number":1,"item":` + doneItem + `}`,
		`data: {"type":"response.completed","sequence_number":2,"response":{"id":"resp_test","status":"completed","output":[` + completedItem + `]}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n")
	ch := make(chan goai.Event, 16)
	model := &goai.Model{ID: "gpt-5-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses, Reasoning: true}
	processStream(body, model, ch)
	close(ch)
	var done *goai.DoneEvent
	for event := range ch {
		if ev, ok := event.(*goai.DoneEvent); ok {
			done = ev
		}
	}
	if done == nil || done.Message == nil {
		t.Fatal("missing done event")
	}
	if len(done.Message.Content) != 1 || done.Message.Content[0].Type != "thinking" {
		t.Fatalf("expected one thinking block, got %#v", done.Message.Content)
	}
	return done
}

func reasoningSignatureMap(t *testing.T, done *goai.DoneEvent) map[string]interface{} {
	t.Helper()
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(done.Message.Content[0].ThinkingSignature), &got); err != nil {
		t.Fatalf("unmarshal thinking signature: %v", err)
	}
	return got
}
