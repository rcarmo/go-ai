package mistral

import (
	"bytes"
	goai "github.com/rcarmo/go-ai"
	"os"
	"strings"
	"testing"
)

func TestMistralStreamToolIdentityMain(t *testing.T) {
	raw, err := os.ReadFile("testdata/tool_identity_main.sse")
	if err != nil {
		t.Fatal(err)
	}
	var done *goai.Message
	starts, ends, deltas := 0, 0, 0
	processSSEStream(bytes.NewReader(raw), &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.ToolCallStartEvent:
			starts++
		case *goai.ToolCallDeltaEvent:
			deltas++
		case *goai.ToolCallEndEvent:
			ends++
		case *goai.DoneEvent:
			done = e.Message
		case *goai.ErrorEvent:
			t.Fatal(e.Err)
		}
	})
	if done == nil || len(done.Content) != 4 || starts != 4 || ends != 4 || deltas != 6 {
		t.Fatal("source tool grouping changed", done, starts, ends, deltas)
	}
	for i, want := range []struct{ id, name string }{{"a12345678", "first"}, {"b12345678", "second"}, {"toolcall0", "indexed"}, {"c12345678", "other"}} {
		block := done.Content[i]
		if block.ID != want.id || block.Name != want.name || block.ArgumentsError != "" || len(block.Arguments) != 0 {
			t.Fatal("identity/arguments mismatch", i, block, want)
		}
	}
}

func TestMistralProtocolFailureMain(t *testing.T) {
	for _, payload := range []string{`{broken}`, `{}`, `{"choices":null}`, `{"choices":[{}]}`, `{"choices":[{"delta":{"tool_calls":[{"id":"bad"}]}}]}`} {
		t.Run(payload, func(t *testing.T) {
			wire := "data: {\"choices\":[{\"delta\":{\"content\":\"before\"}}]}\n\n" + "data: " + payload + "\n\n" + "data: {\"choices\":[{\"delta\":{\"content\":\"after\"}}]}\n\ndata: [DONE]\n\n"
			errors, done := 0, 0
			processSSEStream(strings.NewReader(wire), &goai.Model{}, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					errors++
					if e.Reason != goai.StopReasonError || e.Err == nil || e.Error.StopReason != goai.StopReasonError || len(e.Error.Content) != 1 || e.Error.Content[0].Text != "before" {
						t.Fatal("partial failure lost", e)
					}
				case *goai.DoneEvent:
					done++
				}
			})
			if errors != 1 || done != 0 {
				t.Fatal("malformed frame skipped", errors, done)
			}
		})
	}
}

func TestMistralEmptyAndNullChoiceMain(t *testing.T) {
	wire := "data: \n\n" + "data: {\"choices\":[]}\n\n" + "data: {\"choices\":[null]}\n\n" + "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	var done *goai.Message
	processSSEStream(strings.NewReader(wire), &goai.Model{}, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			done = e.Message
		case *goai.ErrorEvent:
			t.Fatal(e.Err)
		}
	})
	if done == nil || len(done.Content) != 1 || done.Content[0].Text != "ok" {
		t.Fatal("valid empty frames rejected", done)
	}
}
