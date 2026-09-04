package goai_test

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func seedFrameMessage() *goai.Message {
	return &goai.Message{Role: goai.RoleAssistant, Api: "test-api", Provider: "test-provider", Model: "test-model", Usage: &goai.Usage{}, StopReason: goai.StopReasonPending, Timestamp: 1}
}

func mustFrame(t *testing.T, enc *goai.AssistantMessageFrameEncoder, ev goai.Event) *goai.AssistantMessageFrame {
	t.Helper()
	f, err := enc.Encode(ev)
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		t.Fatalf("event %#v did not produce frame", ev)
	}
	return f
}

func TestV0850AssistantMessageFramesPreserveMetadataAndReduce(t *testing.T) {
	partial := seedFrameMessage()
	partial.ProviderThinkingLevel = "high"
	enc := goai.NewAssistantMessageFrameEncoder()
	frames := []goai.AssistantMessageFrame{*mustFrame(t, enc, &goai.StartEvent{Partial: partial})}
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "text", Text: "Hello "})
	frames = append(frames, *mustFrame(t, enc, &goai.TextStartEvent{ContentIndex: 0, Partial: partial}))
	partial.Content[0] = goai.ContentBlock{Type: "text", Text: "Hello world", TextSignature: "sig-text"}
	frames = append(frames, *mustFrame(t, enc, &goai.TextDeltaEvent{ContentIndex: 0, Delta: "incorrect", Partial: partial}))
	frames = append(frames, *mustFrame(t, enc, &goai.TextEndEvent{ContentIndex: 0, Content: "Hello world", Partial: partial}))
	last := frames[len(frames)-1]
	if last.Type != "text_end" || last.Text != "Hello world" || last.TextSignature != "sig-text" {
		t.Fatalf("text_end frame=%#v", last)
	}
	got, err := goai.ReduceAssistantMessageFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderThinkingLevel != "high" || len(got.Content) != 1 || got.Content[0].Text != "Hello world" || got.Content[0].TextSignature != "sig-text" {
		t.Fatalf("reduced=%#v", got)
	}
}

func TestV0850AssistantMessageFramesToolCheckpointAndEndAuthoritative(t *testing.T) {
	partial := seedFrameMessage()
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: "call", Name: "write", Arguments: map[string]interface{}{}})
	enc := goai.NewAssistantMessageFrameEncoder()
	frames := []goai.AssistantMessageFrame{*mustFrame(t, enc, &goai.StartEvent{Partial: partial})}
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallStartEvent{ContentIndex: 0, Partial: partial}))
	partial.Content[0].Arguments = map[string]interface{}{"path": "README.md"}
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallDeltaEvent{ContentIndex: 0, Delta: `{"path":"READ`, Partial: partial}))
	endCall := goai.ToolCall{Type: "toolCall", ID: "final", Name: "write_file", Arguments: map[string]interface{}{"path": "final.md"}, ThoughtSignature: "thought", Namespace: "files"}
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallEndEvent{ContentIndex: 0, ToolCall: endCall, Partial: partial}))
	got, err := goai.ReduceAssistantMessageFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	want := goai.ContentBlock{Type: "toolCall", ID: "final", Name: "write_file", Arguments: map[string]interface{}{"path": "final.md"}, ThoughtSignature: "thought", Namespace: "files"}
	if !reflect.DeepEqual(got.Content[0], want) {
		t.Fatalf("tool=%#v want %#v", got.Content[0], want)
	}
}

func TestV0850AssistantMessageFramesRejectBeforeStartAndWrongKind(t *testing.T) {
	enc := goai.NewAssistantMessageFrameEncoder()
	if _, err := enc.Encode(&goai.TextDeltaEvent{ContentIndex: 0, Delta: "x", Partial: seedFrameMessage()}); err == nil {
		t.Fatal("expected pre-start delta error")
	}
	partial := seedFrameMessage()
	_, _ = enc.Encode(&goai.StartEvent{Partial: partial})
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking", Thinking: "x"})
	if _, err := enc.Encode(&goai.TextStartEvent{ContentIndex: 0, Partial: partial}); err == nil {
		t.Fatal("expected wrong-kind conversion error")
	}
	if _, err := goai.ReduceAssistantMessageFrames([]goai.AssistantMessageFrame{{Type: "text_delta", ContentIndex: 0, Delta: "x"}}); err == nil {
		t.Fatal("expected reduce before-start error")
	}
}

func TestV0850AssistantMessageFramesSnapshotMutableData(t *testing.T) {
	partial := seedFrameMessage()
	partial.Usage.Cost.Total = 1
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: "call", Name: "run", Arguments: map[string]interface{}{"nested": map[string]interface{}{"value": "original"}}})
	enc := goai.NewAssistantMessageFrameEncoder()
	start := mustFrame(t, enc, &goai.StartEvent{Partial: partial})
	tool := mustFrame(t, enc, &goai.ToolCallStartEvent{ContentIndex: 0, Partial: partial})
	partial.Usage.Cost.Total = 99
	partial.Content[0].Arguments["nested"] = "mutated"
	got, err := goai.ReduceAssistantMessageFrames([]goai.AssistantMessageFrame{*start, *tool})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.Cost.Total != 1 {
		t.Fatalf("usage mutated: %#v", got.Usage)
	}
	b, _ := json.Marshal(got.Content[0].Arguments)
	if string(b) != `{"nested":{"value":"original"}}` {
		t.Fatalf("arguments not snapshotted: %s", b)
	}
}
