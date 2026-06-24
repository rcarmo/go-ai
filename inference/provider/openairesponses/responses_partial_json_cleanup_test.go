package openairesponses

import (
	"io"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAIResponsesPartialJSONCleanupRemovesPartialJSONFromPersistedToolCallBlocks(t *testing.T) {
	body := io.NopCloser(strings.NewReader(strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_test","call_id":"call_test","name":"edit","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"README.md\""}`,
		`data: {"type":"response.function_call_arguments.delta","delta":",\"content\":\"updated\"}"}`,
		`data: {"type":"response.function_call_arguments.done","arguments":"{\"path\":\"README.md\",\"content\":\"updated\"}"}`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_test","call_id":"call_test","name":"edit","arguments":"{\"path\":\"README.md\",\"content\":\"updated\"}"}}`,
		`data: {"type":"response.completed","sequence_number":5,"response":{"id":"resp_test","status":"completed"}}`,
	}, "\n\n") + "\n\n"))
	ch := make(chan goai.Event, 16)
	model := &goai.Model{ID: "gpt-5-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	processStream(body, model, ch)
	close(ch)
	var toolCallEnd *goai.ToolCallEndEvent
	var done *goai.DoneEvent
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.ToolCallEndEvent:
			toolCallEnd = e
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if done == nil || done.Message == nil || len(done.Message.Content) != 1 {
		t.Fatalf("unexpected done message: %#v", done)
	}
	persisted := done.Message.Content[0]
	if persisted.Type != "toolCall" {
		t.Fatalf("persisted block type=%q, want toolCall", persisted.Type)
	}
	if persisted.Arguments["path"] != "README.md" || persisted.Arguments["content"] != "updated" {
		t.Fatalf("persisted arguments=%#v", persisted.Arguments)
	}
	if toolCallEnd == nil {
		t.Fatal("expected toolcall_end event")
	}
	if toolCallEnd.ToolCall.Arguments["path"] != "README.md" || toolCallEnd.ToolCall.Arguments["content"] != "updated" {
		t.Fatalf("toolcall_end arguments=%#v", toolCallEnd.ToolCall.Arguments)
	}
}
