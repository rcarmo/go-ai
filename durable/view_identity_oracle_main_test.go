package durable

import (
	"context"
	"reflect"
	"testing"
)

// Pinned f9a26365 harness-view.test.ts:525, content/window portion only. Shared
// docs identity is asserted separately in the pending oracle. These exact
// source window sizes and note kind do not use transcriptNotes' allocator-gap
// adaptation. Native supplies explicit entry IDs and an empty Value object.
func TestMainViewSource525IndependentWindowContents(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, conversation := transcriptFixture(t, b)
		ids := source525Notes(t, h.session, conversation.ID(), 6)
		left, err := conversation.Watch(bg, transcriptBounds(2, 3))
		if err != nil {
			t.Fatal(err)
		}
		defer left.Stop()
		right, err := conversation.Watch(bg, ConversationWindow{Min: source525Int(5)})
		if err != nil {
			t.Fatal(err)
		}
		defer right.Stop()
		a, err := left.Value()
		if err != nil {
			t.Fatal(err)
		}
		z, err := right.Value()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(source525IDs(a), ids[4:]) || !reflect.DeepEqual(source525IDs(z), ids[1:]) {
			t.Fatalf("initial windows: left=%v right=%v ids=%v", source525IDs(a), source525IDs(z), ids)
		}
		leftFrames := make(chan ConversationView, 2)
		rightFrames := make(chan ConversationView, 2)
		if err := left.Start(func(_ context.Context, v ConversationView, _ []Operation) error { leftFrames <- v; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := right.Start(func(_ context.Context, v ConversationView, _ []Operation) error { rightFrames <- v; return nil }); err != nil {
			t.Fatal(err)
		}
		more := source525Notes(t, h.session, conversation.ID(), 1)
		a = watchReceive(t, leftFrames)
		z = watchReceive(t, rightFrames)
		wantLeft := append(append([]ID(nil), ids[4:]...), more...)
		wantRight := append(append([]ID(nil), ids[1:]...), more...)
		if !reflect.DeepEqual(source525IDs(a), wantLeft) || !reflect.DeepEqual(source525IDs(z), wantRight) {
			t.Fatalf("appended windows: left=%v right=%v", source525IDs(a), source525IDs(z))
		}
		if !reflect.DeepEqual(a.Docs, z.Docs) {
			t.Fatal("same-revision windows have different document contents")
		}
	})
}

func source525Notes(t *testing.T, session *Session, conversation ID, count int) []ID {
	t.Helper()
	ids := make([]ID, 0, count)
	for range count {
		checkpointCommit(t, session, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation, Kind: "note", Value: JSON{}}); err != nil {
				return err
			}
			ids = append(ids, id)
			return nil
		})
	}
	return ids
}

func source525Int(n int) *int { return &n }

func source525IDs(value ConversationView) []ID {
	ids := make([]ID, len(value.Entries))
	for i, entry := range value.Entries {
		ids[i] = entry.ID
	}
	return ids
}
