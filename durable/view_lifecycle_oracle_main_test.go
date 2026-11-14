package durable

import (
	"context"
	"errors"
	"testing"
)

// Pinned harness-view.test.ts:330-349: failing one listener leaves its peer
// working. The source's docs reference-identity assertion remains unported:
// native detached ownership is not a substitute for that assertion.
func TestMainViewUpstreamListenerIsolation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		left, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer left.Stop()
		right, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer right.Stop()
		failure := errors.New("listener failed")
		if err = left.Start(func(context.Context, ConversationViewCopy, []Operation) error { return failure }); err != nil {
			t.Fatal(err)
		}
		frames := make(chan ConversationViewCopy, 4)
		if err = right.Start(func(_ context.Context, v ConversationViewCopy, _ []Operation) error { frames <- v; return nil }); err != nil {
			t.Fatal(err)
		}
		upstreamLiveText(t, h.session, c.ID(), "x")
		watchReceive(t, left.Closed())
		end, closed := left.End()
		if !closed || end.Reason != "listener_error" || end.Error != failure {
			t.Fatal(end, closed)
		}
		if v := watchReceive(t, frames); v.Docs["pi.live"]["text"] != "x" {
			t.Fatal(v)
		}
		upstreamLiveText(t, h.session, c.ID(), "y")
		if v := watchReceive(t, frames); v.Docs["pi.live"]["text"] != "y" {
			t.Fatal(v)
		}
		if _, closed := right.End(); closed {
			t.Fatal("healthy peer closed")
		}
		if len(frames) != 0 {
			t.Fatal("extra peer frames")
		}
	})
}

// Pinned harness-view.test.ts:351-380: two notes plus one mounted doc edit are
// one publication; an unmounted doc edit is silent. Preserve kinds/data/text
// inputs and replay/fresh-value/count assertions through real native commits.
func TestMainViewUpstreamAtomicPublication(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		w, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer w.Stop()
		initial, err := w.Value()
		if err != nil {
			t.Fatal(err)
		}
		type delivery struct {
			value ConversationViewCopy
			ops   []Operation
		}
		frames := make(chan delivery, 4)
		if err = w.Start(func(_ context.Context, v ConversationViewCopy, ops []Operation) error {
			frames <- delivery{v, ops}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var ids []ID
		checkpointCommit(t, h.session, func(tx *Tx) error {
			for _, n := range []int{1, 2} {
				id, e := tx.mintID()
				if e != nil {
					return e
				}
				if e = tx.appendEntryRecord(Entry{ID: id, Conversation: c.ID(), Kind: "note", Data: n, HasData: true, Value: JSON{}}); e != nil {
					return e
				}
				ids = append(ids, id)
			}
			live, e := builtin(tx, c.ID(), "pi.live")
			if e != nil {
				return e
			}
			return live.ApplyOperations([]Operation{{"s", []any{"text"}, "done"}})
		})
		f := watchReceive(t, frames)
		if len(f.value.Entries) != 2 || f.value.Entries[0].ID != ids[0] || f.value.Entries[1].ID != ids[1] || f.value.Docs["pi.live"]["text"] != "done" {
			t.Fatal(f.value)
		}
		a, err := dtoObject(initial, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		replica, err := ApplyOperations(map[string]any(a), f.ops, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		want, err := dtoObject(f.value, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if !equalDeltaJSON(replica, map[string]any(want)) {
			t.Fatal("publication replay")
		}
		// Source fresh() acquires and stops a separate watch, not a model context.
		fresh, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		value, err := fresh.Value()
		fresh.Stop()
		if err != nil {
			t.Fatal(err)
		}
		freshJSON, err := dtoObject(value, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if !equalDeltaJSON(map[string]any(want), map[string]any(freshJSON)) {
			t.Fatal("fresh view differs")
		}
		local := checkpointDef(t, DefinitionOptions{Kind: "test.view.extra", Scope: "conversation", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
		checkpointCommit(t, h.session, func(tx *Tx) error {
			doc, e := tx.AcquireDocument(local, c.ID(), nil, nil)
			if e != nil {
				return e
			}
			return doc.ApplyOperations([]Operation{{"s", []any{"x"}, 1}})
		})
		// No sleep or invented callback barrier: the admitted publication created
		// neither a pending frame nor a new delivered value.
		w.sub.mu.Lock()
		pending := len(w.sub.pending)
		w.sub.mu.Unlock()
		if pending != 0 || len(frames) != 0 {
			t.Fatal("unmounted publication delivered", pending, len(frames))
		}
		after, err := w.Value()
		if err != nil {
			t.Fatal(err)
		}
		afterJSON, err := dtoObject(after, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if !equalDeltaJSON(map[string]any(want), map[string]any(afterJSON)) {
			t.Fatal("unmounted edit changed view")
		}
	})
}

func upstreamLiveText(t *testing.T, s *Session, id ID, text string) {
	t.Helper()
	checkpointCommit(t, s, func(tx *Tx) error {
		live, err := builtin(tx, id, "pi.live")
		if err != nil {
			return err
		}
		return live.ApplyOperations([]Operation{{"s", []any{"text"}, text}})
	})
}
