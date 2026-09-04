package goai_test

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestV0850AssistantMessageFramesTextEndAuthoritative(t *testing.T) {
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

func TestV0850AssistantMessageFramesThinkingMetadataAndRedaction(t *testing.T) {
	partial := seedFrameMessage()
	enc := goai.NewAssistantMessageFrameEncoder()
	frames := []goai.AssistantMessageFrame{*mustFrame(t, enc, &goai.StartEvent{Partial: partial})}
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking", Thinking: "[redacted]", ThinkingSignature: "encrypted-start", Redacted: true})
	frames = append(frames, *mustFrame(t, enc, &goai.ThinkingStartEvent{ContentIndex: 0, Partial: partial}))
	partial.Content[0] = goai.ContentBlock{Type: "thinking", Thinking: "[redacted]", ThinkingSignature: "encrypted-final", Redacted: true}
	frames = append(frames, *mustFrame(t, enc, &goai.ThinkingEndEvent{ContentIndex: 0, Content: "[redacted]", Partial: partial}))
	got, err := goai.ReduceAssistantMessageFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	want := goai.ContentBlock{Type: "thinking", Thinking: "[redacted]", ThinkingSignature: "encrypted-final", Redacted: true}
	if !reflect.DeepEqual(got.Content[0], want) {
		t.Fatalf("thinking=%#v want %#v", got.Content[0], want)
	}
}

func TestV0850AssistantMessageFramesToolCheckpointResumeAndEndAuthoritative(t *testing.T) {
	partial := seedFrameMessage()
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: "call", Name: "write", Arguments: map[string]interface{}{}})
	enc := goai.NewAssistantMessageFrameEncoder()
	frames := []goai.AssistantMessageFrame{*mustFrame(t, enc, &goai.StartEvent{Partial: partial})}
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallStartEvent{ContentIndex: 0, Partial: partial}))
	partial.Content[0].Arguments = map[string]interface{}{"path": "README.md"}
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallDeltaEvent{ContentIndex: 0, Delta: `{"path":"README.md"}`, Partial: partial}))
	if frames[len(frames)-1].Type != "toolcall_delta" {
		t.Fatalf("empty-argument tool stream should remain delta, got %#v", frames[len(frames)-1])
	}
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

func TestV0850AssistantMessageFramesLegacyGrammarCheckpointResume(t *testing.T) {
	partial := seedFrameMessage()
	enc := goai.NewAssistantMessageFrameEncoder()
	frames := []goai.AssistantMessageFrame{*mustFrame(t, enc, &goai.StartEvent{Partial: partial})}
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: "call", Name: "bash", Arguments: map[string]interface{}{"input": "a"}})
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallStartEvent{ContentIndex: 0, Partial: partial}))
	partial.Content[0].Arguments = map[string]interface{}{"input": "ab"}
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallDeltaEvent{ContentIndex: 0, Delta: `{"input":"ab`, Partial: partial}))
	partial.Content[0].Arguments = map[string]interface{}{"input": "abc"}
	frames = append(frames, *mustFrame(t, enc, &goai.ToolCallDeltaEvent{ContentIndex: 0, Delta: `c"}`, Partial: partial}))
	if frames[2].Type != "toolcall_checkpoint" || frames[2].JSON != `{"input":"ab` || frames[3].Type != "toolcall_delta" {
		t.Fatalf("legacy checkpoint frames=%#v", frames[2:])
	}
	got, err := goai.ReduceAssistantMessageFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content[0].Arguments["input"] != "abc" {
		t.Fatalf("arguments=%#v", got.Content[0].Arguments)
	}
}

func TestV0850AssistantMessageFramesQueuedTextAndPrefixTrimming(t *testing.T) {
	partial := seedFrameMessage()
	text := goai.ContentBlock{Type: "text", Text: ""}
	partial.Content = append(partial.Content, text)
	events := []goai.Event{&goai.StartEvent{Partial: partial}, &goai.TextStartEvent{ContentIndex: 0, Partial: partial}}
	for _, delta := range []string{"Hel", "lo", " ", "world"} {
		partial.Content[0].Text += delta
		events = append(events, &goai.TextDeltaEvent{ContentIndex: 0, Delta: delta, Partial: partial})
	}
	enc := goai.NewAssistantMessageFrameEncoder()
	var frames []goai.AssistantMessageFrame
	for _, ev := range events {
		f, err := enc.Encode(ev)
		if err != nil {
			t.Fatal(err)
		}
		if f != nil {
			frames = append(frames, *f)
		}
	}
	if gotTypes := frameTypes(frames); !reflect.DeepEqual(gotTypes, []string{"start", "text_start"}) {
		t.Fatalf("frame types=%v", gotTypes)
	}
	got, err := goai.ReduceAssistantMessageFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content[0].Text != "Hello world" {
		t.Fatalf("text=%q", got.Content[0].Text)
	}

	partial2 := seedFrameMessage()
	enc2 := goai.NewAssistantMessageFrameEncoder()
	_ = mustFrame(t, enc2, &goai.StartEvent{Partial: partial2})
	partial2.Content = append(partial2.Content, goai.ContentBlock{Type: "text", Text: "Hel"})
	_ = mustFrame(t, enc2, &goai.TextStartEvent{ContentIndex: 0, Partial: partial2})
	if f, err := enc2.Encode(&goai.TextDeltaEvent{ContentIndex: 0, Delta: "He", Partial: partial2}); err != nil || f != nil {
		t.Fatalf("covered prefix frame=%#v err=%v", f, err)
	}
	f := mustFrame(t, enc2, &goai.TextDeltaEvent{ContentIndex: 0, Delta: "llo", Partial: partial2})
	if f.Delta != "lo" {
		t.Fatalf("delta=%q, want lo", f.Delta)
	}
}

func TestV0850AssistantMessageFramesStrictEncoderState(t *testing.T) {
	enc := goai.NewAssistantMessageFrameEncoder()
	_, err := enc.Encode(&goai.TextDeltaEvent{ContentIndex: 0, Delta: "x", Partial: seedFrameMessage()})
	if err == nil || !strings.Contains(err.Error(), "before start") {
		t.Fatalf("pre-start err=%v", err)
	}
	partial := seedFrameMessage()
	if _, err := enc.Encode(&goai.StartEvent{Partial: partial}); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Encode(&goai.StartEvent{Partial: partial}); err == nil {
		t.Fatal("duplicate start should fail")
	}
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking", Thinking: "x"})
	if _, err := enc.Encode(&goai.TextStartEvent{ContentIndex: 0, Partial: partial}); err == nil || !strings.Contains(err.Error(), "thinking block") {
		t.Fatalf("wrong-kind err=%v", err)
	}
	if _, err := enc.Encode(&goai.ThinkingStartEvent{ContentIndex: 0, Partial: partial}); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Encode(&goai.ThinkingStartEvent{ContentIndex: 0, Partial: partial}); err == nil {
		t.Fatal("duplicate block start should fail")
	}
	if _, err := enc.Encode(&goai.DoneEvent{Reason: goai.StopReasonStop, Message: partial}); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Encode(&goai.ThinkingDeltaEvent{ContentIndex: 0, Delta: "later", Partial: partial}); err == nil || !strings.Contains(err.Error(), "terminal event") {
		t.Fatalf("post-terminal err=%v", err)
	}
	if _, err := goai.NewAssistantMessageFrameEncoder().Encode(&goai.DoneEvent{Reason: goai.StopReasonStop, Message: partial}); err == nil || !strings.Contains(err.Error(), "before start") {
		t.Fatalf("pre-start done err=%v", err)
	}
	failed := seedFrameMessage()
	failed.StopReason = goai.StopReasonError
	failed.ErrorMessage = "setup failed"
	if f, err := goai.NewAssistantMessageFrameEncoder().Encode(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: failed}); err != nil || f != nil {
		t.Fatalf("pre-generation error frame=%#v err=%v", f, err)
	}
}

func TestV0850AssistantMessageFramesStrictReducerGrammar(t *testing.T) {
	if got, err := goai.ReduceAssistantMessageFrames(nil); got != nil || err != nil {
		t.Fatalf("empty reduction got=%#v err=%v", got, err)
	}
	_, err := goai.ReduceAssistantMessageFrames([]goai.AssistantMessageFrame{{Type: "text_delta", ContentIndex: 0, Delta: "x"}})
	if err == nil || !strings.Contains(err.Error(), "before the start frame") {
		t.Fatalf("before-start err=%v", err)
	}
	cases := []struct {
		name   string
		frames []goai.AssistantMessageFrame
		want   string
	}{
		{"duplicate start", []goai.AssistantMessageFrame{{Type: "start", Partial: seedFrameMessage()}, {Type: "start", Partial: seedFrameMessage()}}, "more than one start"},
		{"wrong block kind", []goai.AssistantMessageFrame{{Type: "start", Partial: seedFrameMessage()}, {Type: "toolcall_start", ContentIndex: 0, ToolCall: &goai.ToolCall{Type: "toolCall", ID: "call", Name: "run", Arguments: map[string]interface{}{}}}, {Type: "text_delta", ContentIndex: 0, Delta: "wrong"}}, "expected text block"},
		{"duplicate end", []goai.AssistantMessageFrame{{Type: "start", Partial: seedFrameMessage()}, {Type: "text_start", ContentIndex: 0, Content: &goai.ContentBlock{Type: "text", Text: ""}}, {Type: "text_end", ContentIndex: 0, Text: ""}, {Type: "text_end", ContentIndex: 0, Text: ""}}, "follows the end"},
		{"index gap", []goai.AssistantMessageFrame{{Type: "start", Partial: seedFrameMessage()}, {Type: "text_start", ContentIndex: 1, Content: &goai.ContentBlock{Type: "text", Text: ""}}}, "would leave a gap"},
		{"unknown", []goai.AssistantMessageFrame{{Type: "start", Partial: seedFrameMessage()}, {Type: "mystery"}}, "unknown assistant message frame type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := goai.ReduceAssistantMessageFrames(tc.frames)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
}

func TestV0850AssistantMessageFramesAuthoritativeEndMetadataAndInterleaving(t *testing.T) {
	frames := []goai.AssistantMessageFrame{
		{Type: "start", Partial: seedFrameMessage()},
		{Type: "text_start", ContentIndex: 0, Content: &goai.ContentBlock{Type: "text", Text: "", TextSignature: "stale-text"}},
		{Type: "toolcall_start", ContentIndex: 1, ToolCall: &goai.ToolCall{Type: "toolCall", ID: "call", Name: "lookup", Arguments: map[string]interface{}{}}},
		{Type: "thinking_start", ContentIndex: 2, Content: &goai.ContentBlock{Type: "thinking", Thinking: "", ThinkingSignature: "stale-thinking", Redacted: true}},
		{Type: "text_delta", ContentIndex: 0, Delta: "answer"},
		{Type: "toolcall_delta", ContentIndex: 1, Delta: `{"query":"pi"}`},
		{Type: "thinking_delta", ContentIndex: 2, Delta: "check"},
		{Type: "toolcall_end", ContentIndex: 1, ID: "call-final", Name: "lookup", Arguments: map[string]interface{}{"query": "pi"}},
		{Type: "text_end", ContentIndex: 0, Text: "answer"},
		{Type: "thinking_end", ContentIndex: 2, Text: "check", ThinkingSignature: "", Redacted: boolPtrTest(false)},
	}
	got, err := goai.ReduceAssistantMessageFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	want := []goai.ContentBlock{
		{Type: "text", Text: "answer"},
		{Type: "toolCall", ID: "call-final", Name: "lookup", Arguments: map[string]interface{}{"query": "pi"}},
		{Type: "thinking", Thinking: "check"},
	}
	if !reflect.DeepEqual(got.Content, want) {
		t.Fatalf("content=%#v want %#v", got.Content, want)
	}
}

func TestV0850AssistantMessageFramesSnapshotMutableDataAndPureReduce(t *testing.T) {
	partial := seedFrameMessage()
	partial.Diagnostics = []goai.AssistantMessageDiagnostic{{Type: "test", Timestamp: 2, Details: map[string]interface{}{"value": "original"}}}
	partial.Usage.Cost.Total = 1
	partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: "call", Name: "run", Arguments: map[string]interface{}{"nested": map[string]interface{}{"value": "original"}}})
	enc := goai.NewAssistantMessageFrameEncoder()
	start := mustFrame(t, enc, &goai.StartEvent{Partial: partial})
	tool := mustFrame(t, enc, &goai.ToolCallStartEvent{ContentIndex: 0, Partial: partial})
	partial.Diagnostics[0].Details["value"] = "mutated"
	partial.Usage.Cost.Total = 99
	partial.Content[0].Arguments["nested"] = "mutated"
	got, err := goai.ReduceAssistantMessageFrames([]goai.AssistantMessageFrame{*start, *tool})
	if err != nil {
		t.Fatal(err)
	}
	if got.Diagnostics[0].Details["value"] != "original" || got.Usage.Cost.Total != 1 {
		t.Fatalf("start snapshot mutated: %#v %#v", got.Diagnostics, got.Usage)
	}
	b, _ := json.Marshal(got.Content[0].Arguments)
	if string(b) != `{"nested":{"value":"original"}}` {
		t.Fatalf("arguments not snapshotted: %s", b)
	}
	got.Content[0].Arguments["nested"] = "changed-output"
	b, _ = json.Marshal(tool.ToolCall.Arguments)
	if string(b) != `{"nested":{"value":"original"}}` {
		t.Fatalf("reducer mutated input frames: %s", b)
	}
}

func frameTypes(frames []goai.AssistantMessageFrame) []string {
	out := make([]string, len(frames))
	for i, frame := range frames {
		out[i] = frame.Type
	}
	return out
}

func boolPtrTest(v bool) *bool { return &v }
