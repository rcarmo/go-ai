package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestInputMetadata120PreservesEntryAndUserMessage(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			if len(input.Messages) != 1 || goai.ContentText(input.Messages[0].Content) != "user text" {
				t.Error("custom input missing from model", input.Messages)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		for _, entry := range []*Entry{{Kind: "pi.forged"}, {Kind: ""}, {Kind: "app.input", Head: 1}, {Kind: "app.input", Edits: []ContextEdit{}}} {
			if _, err := conv.Submit(bg, Input{Content: "user text", Entry: entry}); err == nil {
				t.Fatal("invalid input metadata accepted", entry)
			}
		}
		sub, err := conv.Submit(bg, Input{Content: "user text", Entry: &Entry{Kind: "app.input", Data: JSON{"source": "fixture"}, HasData: true}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sub.Wait(bg); err != nil {
			t.Fatal(err)
		}
		snap, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range snap.Entries {
			if entry.Kind == "app.input" {
				found = true
				if !entry.HasData || !equalJSONValue(entry.Data, JSON{"source": "fixture"}) || len(entry.Model) != 1 {
					t.Fatal("custom input lost", entry)
				}
			}
		}
		if !found {
			t.Fatal("custom entry absent")
		}
	})
}
