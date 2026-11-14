package durable

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func transcriptFixture(t *testing.T, b sqliteFixture) (*Harness, *ConversationHandle) {
	t.Helper()
	h := openHarness(t, b.store, Options{})
	c, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.session.Commit(bg, func(tx *Tx) (any, error) { return nil, initializeBuiltins(tx, 1) }); err != nil {
		t.Fatal(err)
	}
	return h, c
}

func transcriptNotes(t *testing.T, h *Harness, count int) []ID {
	t.Helper()
	ids := []ID{}
	_, err := h.session.Commit(bg, func(tx *Tx) (any, error) {
		for i := 0; i < count; i++ {
			// Deliberate allocator gaps: before must identify an actual predecessor.
			if _, err := tx.mintID(); err != nil {
				return nil, err
			}
			id, err := tx.mintID()
			if err != nil {
				return nil, err
			}
			if err = tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "app.note", Value: JSON{"ordinal": i}}); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func transcriptFrame(t *testing.T, frames <-chan ConversationViewCopy) ConversationViewCopy {
	t.Helper()
	select {
	case value := <-frames:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("missing transcript frame")
	}
	return ConversationViewCopy{}
}

// Port of harness-view.test.ts's default newest-50 hydration assertion. The
// wire-level predecessor check also runs against the preceding public API.
func TestMainTranscriptDefaultHydration(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 65)
		value, err := detachedView(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		if len(value.Entries) != 50 {
			t.Fatalf("newest window: got %d entries, want 50", len(value.Entries))
		}
		for i, e := range value.Entries {
			if e.ID != ids[15+i] {
				t.Fatalf("entry[%d]=%d want %d", i, e.ID, ids[15+i])
			}
		}
		wire, err := dtoObject(value, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(wire["before"]) != fmt.Sprint(ids[14]) {
			t.Fatalf("before=%v want actual predecessor %d", wire["before"], ids[14])
		}
	})
}

// Port of "keeps head markers in place". Native kind names and receipt DTOs
// differ; the transcript sequence and summary head remain the source assertions.
func TestMainTranscriptHeadMarkersAppend(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		ids := transcriptNotes(t, h, 3)
		watch, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		initial, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		replay, err := dtoObject(initial, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		frames := make(chan ConversationViewCopy, 4)
		if err = watch.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
			next, err := ApplyOperations(replay, ops, h.session.limits)
			if err != nil {
				return err
			}
			wire, err := dtoObject(value, h.session.limits)
			if err != nil {
				return err
			}
			if len(value.Entries) == 4 && (len(ops) != 1 || ops[0][0] != "p" || fmt.Sprint(ops[0][1]) != "[entries]" || fmt.Sprint(ops[0][2]) != "3" || fmt.Sprint(ops[0][3]) != "0") {
				return fmt.Errorf("summary must append at index 3: %v", ops)
			}
			if !equalJSONValue(next, wire) {
				return fmt.Errorf("transcript ops failed replay")
			}
			replay = wire
			if _, err = detachedView(c, bg); err != nil {
				return err
			}
			frames <- value
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var summary ID
		if _, err = h.session.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			summary, err = tx.mintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: summary, Conversation: 1, Kind: "app.summary", Head: ids[1], Value: JSON{}})
		}); err != nil {
			t.Fatal(err)
		}
		value := transcriptFrame(t, frames)
		if len(value.Entries) != 4 || value.Entries[3].ID != summary || value.Entries[3].Head != ids[1] {
			t.Fatalf("summary replaced transcript: %+v", value.Entries)
		}
		transcriptNotes(t, h, 1)
		value = transcriptFrame(t, frames)
		if len(value.Entries) != 5 || value.Entries[3].ID != summary {
			t.Fatalf("post-summary transcript: %+v", value.Entries)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) (any, error) { return nil, appendReset(tx, 1, "reset") }); err != nil {
			t.Fatal(err)
		}
		value = transcriptFrame(t, frames)
		if len(value.Entries) != 6 || value.Entries[5].Kind != "pi.reset" {
			t.Fatalf("reset replaced transcript: %+v", value.Entries)
		}
		for i := range ids {
			if value.Entries[i].ID != ids[i] {
				t.Fatal("old transcript entry lost")
			}
		}
		active, err := c.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(active.Entries) != 1 || active.Entries[0].Kind != "pi.reset" {
			t.Fatalf("transcript leaked into model context: %+v", active.Entries)
		}
		point, err := detachedView(c, bg)
		if err != nil || len(point.Entries) != 6 {
			t.Fatal("point transcript lost history", point, err)
		}
	})
}
