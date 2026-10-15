package pimessages

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestPiMessages130ToolEndPreservesArgumentError(t *testing.T) {
	convert := newConverter(&goai.Model{Api: goai.ApiPiMessages, Provider: "test", ID: "model"})
	if _, _, err := convert([]byte(`{"type":"toolcall_start","contentIndex":0,"id":"call","name":"leaf"}`)); err != nil {
		t.Fatal(err)
	}
	event, _, err := convert([]byte(`{"type":"toolcall_end","contentIndex":0,"toolCall":{"id":"call","name":"leaf","arguments":{},"argumentsError":"invalid object\n[1]"}}`))
	if err != nil {
		t.Fatal(err)
	}
	end, ok := event.(*goai.ToolCallEndEvent)
	if !ok || end.ToolCall.ArgumentsError != "invalid object\n[1]" || end.Partial.Content[0].ArgumentsError != end.ToolCall.ArgumentsError {
		t.Fatal(event)
	}
}
