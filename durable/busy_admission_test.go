package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestBusyRejectIsAtomicAndDedupPrecedesBusyCheck(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			go func() { close(entered); <-release; ch <- terminal("answer"); close(ch) }()
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation := root(t, h, ref)
		original, err := conversation.Submit(bg, Input{Content: "first", RequestID: "original"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		duplicate, err := conversation.Submit(bg, Input{Content: "ignored", RequestID: "original", WhenBusy: "reject"})
		if err != nil || duplicate.ID() != original.ID() {
			t.Fatal("busy dedup rejected", duplicate, err)
		}
		if _, err := conversation.Submit(bg, Input{Content: "new", RequestID: "new", WhenBusy: "reject"}); !errors.Is(err, ErrConversationBusy) {
			t.Fatal("busy rejection", err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq || after.HighWater != before.HighWater || len(after.Submissions) != 1 {
			t.Fatal("busy rejection wrote/reserved", after, before, err)
		}
		releaseTaskGate(release)
		waitSubmission(t, original)
	})
}
func TestIdlePassiveEntryRetainsPayloadAndRejectsStaleHead(t *testing.T) {
	store, _ := NewMemory()
	h := openHarness(t, store, Options{})
	// Passive writes require an agent even though they never dispatch a model.
	conversation, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: goai.ProviderOpenAI, ID: "unused"}})
	if err != nil {
		t.Fatal(err)
	}
	draft := Entry{Kind: "app.note", Value: JSON{"text": "retained"}, Model: []MessageReceipt{userReceipt("model contribution")}}
	write, err := conversation.Submit(bg, Input{Type: "write", Entry: &draft})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, write)
	draft.Value["text"] = "caller mutation"
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var old ID
	for _, entry := range state.Entries {
		if entry.Kind == "app.note" {
			old = entry.ID
			if entry.Value["text"] != "retained" || len(entry.Model) != 1 {
				t.Fatal("passive draft lost", entry)
			}
		}
	}
	if old == 0 {
		t.Fatal("passive draft missing")
	}
	if err := conversation.Reset(bg, "fresh"); err != nil {
		t.Fatal(err)
	}
	stale, err := conversation.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "pi.compaction", Head: old, Value: JSON{}, Model: []MessageReceipt{userReceipt("obsolete")}}})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, stale); result.Submission.Status != "failed" || result.Submission.Value["errorCode"] != "stale" {
		t.Fatal(result)
	}
	view, err := conversation.ContextView(bg, 0)
	if err != nil || view.Head == nil || view.Head.Kind != "pi.reset" {
		t.Fatal("stale head revived", view, err)
	}
}
