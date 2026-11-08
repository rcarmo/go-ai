package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source f9a26365 live.ts:290-318 and restart-failure204/554. Native pi.live
// retains a private failedRun queue gate; assert it explicitly, rather than
// hiding arbitrary fields. first is an actual placed entry ID, not a clock.
func mainRequireRunFailure(t *testing.T, state Snapshot, conversation, task, input, first ID, reason, detail string, held bool) {
	t.Helper()
	if entry, ok := state.Entries[first]; !ok || entry.Conversation != conversation || legacyEntryValue(entry)["role"] != "user" {
		t.Fatal("failure first is not its persisted user entry", first)
	}
	var live JSON
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == conversation {
			live = doc.Value
		}
	}
	want := JSON{"runFailure": JSON{"taskId": task, "inputs": []any{input}, "reason": reason, "detail": detail, "first": first}}
	if held {
		want["failedRun"] = true
	}
	wire, err := dtoObject(want, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !equalJSONValue(live, wire) {
		t.Errorf("failed run projection: got %v; want %v", live, wire)
	}
}

// Native lifecycle control, not another complete source declaration: an
// automatic continuation retains the old failure, but starts its own entry
// boundary (unlike an explicit source continue submission, still unported).
// A second failure replaces the first; reopen retains it; fresh work clears it.
func TestMainRunFailureContinuationAndReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		entered, release := make(chan struct{}), make(chan struct{})
		secondEntered, secondRelease := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release, secondRelease)
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			n := calls.Add(1)
			go func() {
				defer close(ch)
				if n == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return
					}
				}
				if n == 2 {
					close(secondEntered)
					select {
					case <-secondRelease:
					case <-ctx.Done():
						return
					}
				}
				if n <= 2 {
					msg := terminal("failed").Message
					msg.StopReason, msg.ErrorMessage = goai.StopReasonError, fmt.Sprintf("failure %d", n)
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg}
				} else {
					ch <- terminal("fresh answer")
				}
			}()
			return ch
		})
		options.Settings = &HarnessSettings{AfterFailedRun: "continue"}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		next, err := c.Submit(bg, Input{Content: "next"})
		if err != nil {
			t.Fatal(err)
		}
		close(release)
		original := waitSubmission(t, first)
		awaitTaskSignal(t, secondEntered)
		active, err := next.Status(bg)
		if err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		want, err := dtoObject(JSON{"taskId": original.Task.ID, "inputs": []any{first.ID()}, "reason": "model_error", "detail": "failure 1", "first": original.Record.Entry}, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == "pi.live" && doc.Owner == c.ID() {
				if !equalJSONValue(doc.Value["runFailure"], want) {
					t.Error("automatic continuation lost previous failure", doc.Value)
				}
				if active.Entry == 0 || submissionEntryID(generationObject(doc.Value["run"])["first"]) != active.Entry {
					t.Error("automatic continuation must start at its own placed entry", doc.Value, active)
				}
			}
		}
		close(secondRelease)
		failed := waitSubmission(t, next)
		idleFailed140(t, h)
		if original.Record.Entry == 0 || original.Record.Entry == failed.Record.Entry {
			t.Fatal("independent placed input entries required")
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainRequireRunFailure(t, state, c.ID(), failed.Task.ID, next.ID(), failed.Record.Entry, "model_error", "failure 2", false)
		// Snapshots remain detached; editing returned failure metadata cannot
		// alter the persisted record subsequently adopted by another host.
		for _, doc := range state.Documents {
			if doc.Kind == "pi.live" && doc.Owner == c.ID() {
				generationObject(doc.Value["runFailure"])["detail"] = "caller mutation"
			}
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, utf16ReopenStore(t, b.store), options)
		c, err = h.Conversation(bg, c.ID())
		if err != nil {
			t.Fatal(err)
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainRequireRunFailure(t, state, c.ID(), failed.Task.ID, next.ID(), failed.Record.Entry, "model_error", "failure 2", false)
		if calls.Load() != 2 {
			t.Fatal("reopen re-dispatched a failed run", calls.Load())
		}
		fresh, err := c.Submit(bg, Input{Content: "fresh"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, fresh)
		idleFailed140(t, h)
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == "pi.live" && doc.Owner == c.ID() && len(doc.Value) != 0 {
				t.Error("fresh answered run retained failure history", doc.Value)
			}
		}
	})
}
