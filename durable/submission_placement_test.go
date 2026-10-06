package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestSubmissionIdleAdmissionPlacesEntryAtomicallyAndPublicLifecycle(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			close(entered)
			go func() { <-release; ch <- terminal("answer"); close(ch) }()
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		root := root(t, h, ref)
		sub, err := root.Submit(bg, Input{Content: "placed at admission"})
		if err != nil {
			t.Fatal(err)
		}
		status, err := sub.Status(bg)
		if err != nil || status.Type != "input" || status.Status != "placed" || status.Entry == 0 {
			t.Fatal(status, err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		entry, ok := state.Entries[status.Entry]
		if !ok || entry.Conversation != root.ID() {
			t.Fatal("admission lacks user entry", entry)
		}
		if result, err := sub.Abort(bg); err != nil || result != "already_placed" {
			t.Fatal(result, err)
		}
		awaitTaskSignal(t, entered)
		queued, err := root.Submit(bg, Input{Content: "withdrawn before placement"})
		if err != nil {
			t.Fatal(err)
		}
		queuedStatus, err := queued.Status(bg)
		if err != nil || queuedStatus.Status != "queued" || queuedStatus.Entry != 0 {
			t.Fatal(queuedStatus, err)
		}
		if result, err := queued.Abort(bg); err != nil || result != "aborted" {
			t.Fatal(result, err)
		}
		releaseTaskGate(release)
		waitSubmission(t, sub)
		status, err = sub.Status(bg)
		if err != nil || status.Status != "done" || status.Entry == 0 || status.Answer == 0 {
			t.Fatal(status, err)
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := state.Entries[status.Answer]; !ok {
			t.Fatal("answer reference missing", status)
		}
		// Exactly one input and one assistant receipt: prepare must not append the
		// already admitted user entry a second time.
		if len(state.Entries) != 2 {
			t.Fatal("admitted input duplicated", len(state.Entries))
		}
		write, err := root.Submit(bg, Input{Type: "write", Content: "passive"})
		if err != nil {
			t.Fatal(err)
		}
		written, err := write.Status(bg)
		if err != nil || written.Type != "write" || written.Status != "done" || written.Entry == 0 || written.Answer != 0 {
			t.Fatal(written, err)
		}
	})
}
