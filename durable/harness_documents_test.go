package durable

import (
	"errors"
	"testing"
)

func TestHarnessTypedDocumentForwardingHistoryStateWatchAndNoSchedule(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		notes := mustDefinition(t, DefinitionOptions{Kind: "app.forward.notes", Scope: "conversation", Version: 1, History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{"text": "initial"}, nil }})
		h := taskTestHarness(t, b.store)
		root, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		var at ID
		_, err = root.Commit(bg, func(tx *Tx) error {
			doc, err := tx.AcquireDocument(notes, root.ID(), nil, nil)
			if err != nil {
				return err
			}
			if err = doc.Set(JSON{"text": "first"}); err != nil {
				return err
			}
			at, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: at, Conversation: root.ID(), Kind: "note", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = root.Commit(bg, func(tx *Tx) error {
			doc, err := tx.AcquireDocument(notes, root.ID(), nil, nil)
			if err != nil {
				return err
			}
			return doc.Set(JSON{"text": "second"})
		})
		if err != nil {
			t.Fatal(err)
		}
		// Harness is a Session facade; passive unbound commits and atomic
		// publications must not require reaching into its private Session.
		baseline, subscription, err := h.SubscribeCommits(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer subscription.Stop()
		if _, err := h.Commit(bg, func(*Tx) error { return nil }); err != nil {
			t.Fatal(err)
		}
		before, err := h.Snapshot(bg)
		if baseline.Seq != before.Seq {
			t.Fatal("empty facade commit wrote")
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, read := range []func() (JSON, bool, error){func() (JSON, bool, error) { return h.SnapshotDefinition(bg, notes, root.ID(), nil) }, func() (JSON, bool, error) { return root.SnapshotDefinition(bg, notes, nil) }} {
			value, found, err := read()
			if err != nil || !found || value["text"] != "second" {
				t.Fatal(value, found, err)
			}
			value["text"] = "mutated"
		}
		entryToken, err := DefineEntry("note")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.Commit(bg, func(tx *Tx) error {
			read, found, err := tx.TypedEntry(entryToken, root.ID(), at)
			if err != nil || !found || read.ID != at {
				t.Fatal(read, found, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		entry, found, err := h.TypedEntry(bg, entryToken, root.ID(), at)
		if err != nil || !found || entry.ID != at {
			t.Fatal(entry, found, err)
		}
		value, found, err := root.SnapshotDefinitionAsOf(bg, notes, nil, at)
		if err != nil || !found || value["text"] != "first" {
			t.Fatal(value, found, err)
		}
		state, found, err := h.DocumentState(bg, notes, root.ID(), nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		defer state.Dispose()
		value, err = state.Value(bg)
		if err != nil || value["text"] != "second" {
			t.Fatal(value, err)
		}
		watch, found, err := root.WatchDefinition(bg, notes, nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		value, err = watch.Value()
		if err != nil || value["text"] != "second" {
			t.Fatal(value, err)
		}
		if err := h.UnloadDocuments(bg); err != nil {
			t.Fatal(err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || before.Seq != after.Seq || h.scheduler.enabled.Load() {
			t.Fatal("forwarding wrote/resumed", before.Seq, after.Seq, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, watch.Closed())
		awaitTaskSignal(t, subscription.Closed())
		if _, _, err := h.TypedEntry(bg, entryToken, root.ID(), at); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if _, _, err := h.SubscribeCommits(bg); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if _, err := h.Commit(bg, func(*Tx) error { return nil }); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if _, _, err := h.SnapshotDefinition(bg, notes, root.ID(), nil); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if _, _, err := h.DocumentState(bg, notes, root.ID(), nil); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
		if _, _, err := root.WatchDefinition(bg, notes, nil); !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	})
}
