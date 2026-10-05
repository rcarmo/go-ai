package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestToolImageContentPersistsAndReachesSuccessor(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "image", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "image.test", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{Blocks: []goai.ContentBlock{{Type: "image", Data: "AQID", MimeType: "image/png"}}}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- toolAnswer("image-call", "image", JSON{})
			} else {
				found := false
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						for _, block := range message.Content {
							if block.Type == "image" && block.Data == "AQID" && block.MimeType == "image/png" {
								found = true
							}
						}
					}
				}
				if !found {
					t.Error("successor lost image block", input.Messages)
				}
				ch <- terminal("image seen")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "look"})
		if err != nil {
			t.Fatal(err)
		}
		if settled := waitSubmission(t, sub); settled.Submission.Status != "done" {
			t.Fatal(settled)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		recovered, err := second.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		view, err := recovered.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, message := range view.Messages {
			for _, block := range message.Content {
				if block.Type == "image" && block.Data == "AQID" {
					found = true
				}
			}
		}
		if !found || calls.Load() != 2 {
			t.Fatal("image receipt/replay", view, calls.Load())
		}
	})
}
