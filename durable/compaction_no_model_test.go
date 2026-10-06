package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCompactionMissingModelFailsBeforeNoCutOrHooks(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			t.Error("missing model dispatched")
			return nil
		})
		options.Models = func(goai.Provider, string) *goai.Model { return nil }
		hooks := 0
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "compaction-no-model", CompactionHooks: CompactionHooks{BeforeCompact: func(context.Context, CompactionInput, *HookAPI) (*CompactionDecision, error) {
			hooks++
			summary := "hook summary"
			return &CompactionDecision{Summary: &summary}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		id, err := conversation.Compact(bg, CompactionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "failed" || record.State.Outcome.Error.Message != "Model openai/durable-test is not available" || record.State.Outcome.Error.Detail.Value.(map[string]any)["reason"] != "no_model" || hooks != 0 {
			t.Fatal(record, hooks)
		}
	})
}
