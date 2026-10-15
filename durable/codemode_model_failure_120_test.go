package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120ModelFailureHeaderAndTranscript(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		extension, err := NewCodemodeExtension(engine)
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "failure-call", Name: "codemode", Arguments: map[string]any{"code": `text("before");console.warn("retained");store("must_not_commit",true);throw Error("boom");`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				last := input.Messages[len(input.Messages)-1]
				text := goai.ContentText(last.Content)
				if last.Role != goai.RoleToolResult || !last.IsError || last.ToolName != "codemode" || last.ToolCallID != "failure-call" {
					t.Error("failure result identity", last)
				}
				for _, part := range []string{"Script failed\nWall time ", " seconds\nOutput:\n", "before", "retained", "Script error:", "boom"} {
					if !strings.Contains(text, part) {
						t.Error("failure output missing", part, text)
					}
				}
				if strings.Contains(text, "Script completed") {
					t.Error("success header on failed script", text)
				}
				ch <- terminal("failure handled")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "fail script"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		result, err := sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || requests.Load() != 2 {
			t.Fatal(result, err, requests.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == "pi.codemode.store" {
				values, ok := doc.Value["values"].(map[string]any)
				if !ok {
					t.Fatal("store values missing", doc)
				}
				if _, ok := values["must_not_commit"]; ok {
					t.Fatal("failed script committed store", doc)
				}
			}
		}
		view, err := conv.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		results := 0
		for _, message := range view.Messages {
			if message.Role == goai.RoleToolResult && message.ToolName == "codemode" {
				results++
				if !message.IsError || !strings.HasPrefix(goai.ContentText(message.Content), "Script failed\n") {
					t.Fatal("persisted failure lost", message)
				}
			}
		}
		if results != 1 {
			t.Fatal("failure transcript duplicated", results)
		}
	})
}
