package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompactionLiveStatusRetrySnapshotReopenAndRemoval(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var now atomic.Int64
		now.Store(1000)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
			} else {
				ch <- terminal("summary")
			}
			close(ch)
			return ch
		})
		options.Now = now.Load
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		zero := 0
		id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokensOverride: &zero})
		if err != nil {
			t.Fatal(err)
		}
		state := waitCompactionCheckpoint(t, h, id, "retry")
		var status map[string]any
		for _, document := range state.Documents {
			if document.Kind == "pi.live" && document.Owner == conversation.ID() {
				items := document.Value["compactions"].([]any)
				if len(items) != 1 {
					t.Fatal(items)
				}
				status = items[0].(map[string]any)
			}
		}
		if status["reason"] != "manual" || status["blocking"] != false || !equalJSONValue(status["attempt"], 1) || status["retry"].(map[string]any)["error"] != "503 overloaded" {
			t.Fatal(status)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		stream, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := stream.Snapshot()
		if err != nil || len(snapshot.Documents["pi.live"]["compactions"].([]any)) != 1 {
			t.Fatal(snapshot, err)
		}
		now.Store(3000)
		waitPublicTask(t, h, id)
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, document := range state.Documents {
			if document.Kind == "pi.live" && document.Owner == conversation.ID() {
				if _, exists := document.Value["compactions"]; exists {
					t.Fatal("terminal compaction status retained", document)
				}
			}
		}
		stream.Stop()
	})
}

func waitCompactionCheckpoint(t *testing.T, h *Harness, id ID, phase string) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		state, err := h.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		record, err := CanonicalTask(state.Tasks[id], h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if record.State.Checkpoint["phase"] == phase {
			return state
		}
		select {
		case <-ctx.Done():
			t.Fatal("compaction phase missing", phase)
		default:
			runtime.Gosched()
		}
	}
}
