package durable

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestConversationWatchActiveHeadDeltasDetachedAndOffLine(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		h := openHarness(t, b.store, Options{})
		conversation, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			if err := initializeBuiltins(tx, 1); err != nil {
				return err
			}
			for _, text := range []string{"first", "second"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.raw", Value: JSON{"text": text}}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		watch, err := conversation.Watch(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		baseline, err := watch.Value()
		if err != nil || len(baseline.Entries) != 2 || len(baseline.Docs) != 3 {
			t.Fatal(baseline, err)
		}
		initial, err := dtoObject(baseline, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		baseline.Entries[0].Value["text"] = "caller mutation"
		frames := make(chan ConversationView, 4)
		if err := watch.Start(func(_ context.Context, value ConversationView, ops []Operation) error {
			next, err := ApplyOperations(initial, ops, h.session.limits)
			if err != nil {
				return err
			}
			wire, err := dtoObject(value, h.session.limits)
			if err != nil {
				return err
			}
			if !equalJSONValue(next, wire) {
				t.Error("watch ops do not reconstruct value", ops, next, wire)
			}
			initial = wire
			if _, err := conversation.View(bg); err != nil {
				return err
			}
			value.Docs["pi.live"]["escaped"] = true
			frames <- value
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			live, err := builtin(tx, 1, "pi.live")
			if err != nil {
				return err
			}
			if err = live.Set(JSON{"detail": "committed"}); err != nil {
				return err
			}
			return appendReset(tx, 1, "reset")
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case frame := <-frames:
			if len(frame.Entries) != 1 || frame.Entries[0].Kind != "pi.reset" || frame.Docs["pi.live"]["detail"] != "committed" {
				t.Fatal(frame)
			}
		case <-time.After(3 * time.Second):
			end, _ := watch.End()
			t.Fatal("view callback missing", end)
		}
		current, err := watch.Value()
		if err != nil || current.Docs["pi.live"]["escaped"] != nil {
			t.Fatal("watch callback mutation escaped", current, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		<-watch.Closed()
		end, _ := watch.End()
		if end.Reason != "session_closed" {
			t.Fatal(end)
		}
	})
}

func TestConversationWatchOverflowReplacesWithLatestActiveSnapshot(t *testing.T) {
	store, _ := NewMemory()
	h := openHarness(t, store, Options{})
	conversation, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.session.Commit(bg, func(tx *Tx) error { return initializeBuiltins(tx, 1) })
	if err != nil {
		t.Fatal(err)
	}
	watch, err := conversation.Watch(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	// Leave the listener unattached while overflowing its bounded commit queue.
	for i := 0; i < 105; i++ {
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			live, err := builtin(tx, 1, "pi.live")
			if err != nil {
				return err
			}
			return live.Set(JSON{"step": i})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	reset := false
	latest := make(chan struct{}, 1)
	if err := watch.Start(func(_ context.Context, value ConversationView, ops []Operation) error {
		if len(ops) > 0 && ops[0][0] == "r" {
			reset = true
		}
		if fmt.Sprint(value.Docs["pi.live"]["step"]) == "104" {
			select {
			case latest <- struct{}{}:
			default:
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-latest:
	case <-time.After(3 * time.Second):
		end, _ := watch.End()
		t.Fatal("latest overflow snapshot missing", end)
	}
	// Stop joins an active listener before reading its accumulated flags.
	watch.Stop()
	if !reset {
		t.Fatal("overflow did not use a root replacement")
	}
	value, err := watch.Value()
	if err != nil || fmt.Sprint(value.Docs["pi.live"]["step"]) != "104" {
		t.Fatal(value, err)
	}
}
