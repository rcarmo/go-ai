package mistral

import (
	"bytes"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"os"
	"reflect"
	"testing"
)

func TestMistralStructuredContentReplayMain(t *testing.T) {
	raw, err := os.ReadFile("testdata/content_main.sse")
	if err != nil {
		t.Fatal(err)
	}
	model := &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	var events []string
	var done *goai.Message
	processSSEStream(bytes.NewReader(raw), model, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.ThinkingStartEvent:
			events = append(events, "thinking_start")
		case *goai.ThinkingDeltaEvent:
			events = append(events, "thinking_delta:"+e.Delta)
		case *goai.ThinkingEndEvent:
			events = append(events, "thinking_end:"+e.Content)
		case *goai.TextStartEvent:
			events = append(events, "text_start")
		case *goai.TextDeltaEvent:
			events = append(events, "text_delta:"+e.Delta)
		case *goai.TextEndEvent:
			events = append(events, "text_end:"+e.Content)
		case *goai.DoneEvent:
			done = e.Message
		}
	})
	want := []string{"thinking_start", "thinking_delta:A😃", "thinking_delta:B", "thinking_end:A😃B", "text_start", "text_delta: answer ", "text_delta:more", "text_end: answer more"}
	if !reflect.DeepEqual(events, want) {
		t.Fatal("lifecycle/source content mismatch", events, want)
	}
	if done == nil || len(done.Content) != 2 || done.Content[0].Thinking != "A😃B" || done.Content[1].Text != " answer more" {
		t.Fatal("content lost/split", done)
	}
	request := buildRequest(model, &goai.Context{Messages: []goai.Message{*done}}, &goai.StreamOptions{})
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Messages []struct {
			Prefix  *bool                `json:"prefix"`
			Content []mistralContentPart `json:"content"`
		}
	}
	if err = json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	expected := []mistralContentPart{{Type: "thinking", Thinking: []mistralThinkingText{{Type: "text", Text: "A😃B"}}}, {Type: "text", Text: " answer more"}}
	if len(wire.Messages) != 1 || wire.Messages[0].Prefix == nil || *wire.Messages[0].Prefix || !reflect.DeepEqual(wire.Messages[0].Content, expected) {
		t.Fatal("replay flattened thinking", string(encoded))
	}
}

func TestMistralEmptyAssistantReplayMain(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	request := buildRequest(model, &goai.Context{Messages: []goai.Message{{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Content: []goai.ContentBlock{{Type: "thinking", Thinking: " \t"}, {Type: "text", Text: "\ufeff"}}}}}, &goai.StreamOptions{})
	if len(request.Messages) != 0 {
		t.Fatal("empty replay emitted", request.Messages)
	}
}

func TestMistralContinuedToolClosesTextMain(t *testing.T) {
	wire := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tool12345","function":{"name":"lookup","arguments":"{}"}}]}}]}

data: {"choices":[{"delta":{"content":"a"}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":" "}}]}}]}

data: {"choices":[{"delta":{"content":"b"},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	var texts []string
	var done *goai.Message
	processSSEStream(bytes.NewBufferString(wire), &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.TextEndEvent:
			texts = append(texts, e.Content)
		case *goai.DoneEvent:
			done = e.Message
		}
	})
	if !reflect.DeepEqual(texts, []string{"a", "b"}) || done == nil || len(done.Content) != 3 {
		t.Fatal("continued tool did not terminate text block", texts, done)
	}
}
