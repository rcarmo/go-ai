package durable

import (
	"context"
	"errors"
	"testing"
)

func TestMainSessionLegacyReadsExcludeUnpublishedGroup(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, ids := groupEntryBatches(t, b.store, 2)
		doc := mint(t, b.store)
		batches[0].Writes = append(batches[0].Writes, Write{Op: "put-document", Document: &Document{ID: doc, Scope: "session", Kind: "legacy.image", Version: 1, Value: JSON{"x": 1}}})
		h := taskTestHarness(t, b.store)
		s := h.session
		conversation, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		result, err := s.core.commitBatches(bg, batches, nil, true)
		if err != nil || result.stored != 2 {
			t.Fatal(result, err)
		}
		entry, err := DefineEntry("group")
		if err != nil {
			t.Fatal(err)
		}
		def := mustDefinition(t, DefinitionOptions{Kind: "legacy.image", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
		t.Run("entries", func(t *testing.T) {
			entries, err := conversation.Entries(bg, EntryQuery{}, 10, nil)
			if err != nil || len(entries.Items) != 0 {
				t.Fatalf("unpublished entries=%d err=%v", len(entries.Items), err)
			}
		})
		t.Run("typed-entry", func(t *testing.T) {
			record, ok, err := s.Entry(bg, ids[0])
			if err != nil || ok || entry.Is(record.Entry) {
				t.Fatalf("unpublished typed entry=%v err=%v", ok, err)
			}
		})
		t.Run("definition", func(t *testing.T) {
			_, ok, err := detachedDefinitionSnapshot(s, bg, def, 0, nil)
			if err != nil || ok {
				t.Fatalf("unpublished definition=%v err=%v", ok, err)
			}
		})
		t.Run("subscription-baseline", func(t *testing.T) {
			image, sub, err := s.SubscribeCommits(bg)
			if err != nil {
				t.Fatal(err)
			}
			defer sub.Stop()
			if len(image.Entries) != 0 {
				t.Fatalf("unpublished baseline entries=%d", len(image.Entries))
			}
		})
	})
}

func TestMainDocumentStateKeepsPublishedValue(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		id := mint(t, b.store)
		record := Document{ID: id, Scope: "session", Kind: "legacy.state", Version: 1, Value: JSON{"x": 1}}
		apply(t, b.store, Write{Op: "put-document", Document: &record})
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		def := mustDefinition(t, DefinitionOptions{Kind: record.Kind, Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
		state, ok, err := s.DocumentState(bg, def, 0, nil)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		defer state.Dispose()
		record.Value = JSON{"x": 2}
		if _, err := s.core.apply(bg, Batch{Writes: []Write{{Op: "put-document", Document: &record}}}, true); err != nil {
			t.Fatal(err)
		}
		value, err := state.Value(bg)
		if err != nil || !equalJSONValue(value["x"], 1) {
			t.Fatal("unpublished document value", value, err)
		}
		value["x"] = "mutated"
		again, err := state.Value(bg)
		if err != nil || !equalJSONValue(again["x"], 1) {
			t.Fatal("document value alias", again, err)
		}
	})
}

func TestMainLegacyEntriesPagingAdmissionAndAdapter(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 2)
		for _, batch := range batches {
			if _, err := b.store.Apply(bg, batch); err != nil {
				t.Fatal(err)
			}
		}
		h := taskTestHarness(t, b.store)
		c, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		first, err := c.Entries(bg, EntryQuery{Order: Ascending}, 1, nil)
		if err != nil || len(first.Items) != 1 || first.Next == nil {
			t.Fatal(first, err)
		}
		stored, found, err := h.session.Entry(bg, first.Items[0].ID)
		if err != nil || !found || stored.CommitSeq != 2 {
			t.Fatal(stored, found, err)
		}
		first.Items[0].Value["text"] = "mutated"
		second, err := c.Entries(bg, EntryQuery{Order: Ascending}, 1, first.Next)
		if err != nil || len(second.Items) != 1 || second.Items[0].Value["text"] == "mutated" {
			t.Fatal(second, err)
		}
		stored, found, err = h.session.Entry(bg, second.Items[0].ID)
		if err != nil || !found || stored.CommitSeq != 3 {
			t.Fatal(stored, found, err)
		}
		again, err := c.Entries(bg, EntryQuery{Order: Ascending}, 1, nil)
		if err != nil || again.Items[0].Value["text"] == "mutated" {
			t.Fatal(again, err)
		}
		for _, limit := range []int{0, h.session.limits.MaxPage + 1} {
			if _, err := c.Entries(bg, EntryQuery{}, limit, nil); err == nil {
				t.Fatal("invalid limit accepted")
			}
		}
		if _, err := c.Entries(bg, EntryQuery{}, 1, Cursor{"order": "up", "after": 1}); err == nil {
			t.Fatal("invalid cursor accepted")
		}
		// Hold the Session line to establish that cancellation, rather than a raw
		// backend read, ends this admission. No timer/scheduler ordering assumption.
		if h.session.line.acquire(bg) != nil {
			t.Fatal("could not hold the Session line")
		}
		ctx, cancel := context.WithCancel(bg)
		cancel()
		_, err = c.Entries(ctx, EntryQuery{}, 1, nil)
		h.session.leave()
		if !errors.Is(err, context.Canceled) {
			t.Fatal("admission did not cancel", err)
		}
		adapter := &mainLegacyEntriesAdapter{Storage: b.store, session: h.session}
		h.session.taskBookkeeping(func() { h.session.store = adapter })
		values, err := c.Entries(bg, EntryQuery{}, 1, nil)
		if err != nil || len(values.Items) != 1 || values.Items[0].Value["text"] != "adapter" || adapter.calls != 1 || !adapter.lineOwned {
			t.Fatal(values, err, adapter.calls)
		}
		adapter.err = storageRequest("adapter request")
		if _, err = c.Entries(bg, EntryQuery{}, 1, nil); err != adapter.err {
			t.Fatal("adapter error lost", err)
		}
		if _, err = h.session.Snapshot(bg); err != nil {
			t.Fatal("request rejection poisoned Session", err)
		}
	})
}

type mainLegacyEntriesAdapter struct {
	Storage
	session   *Session
	lineOwned bool
	calls     int
	err       error
}

func (a *mainLegacyEntriesAdapter) ScanEntries(ctx context.Context, query EntryQuery, n int, cursor Cursor) (Page[Entry], error) {
	a.calls++
	a.lineOwned = a.session.line.held()
	if a.err != nil {
		return Page[Entry]{}, a.err
	}
	entries, err := a.Storage.ScanEntries(ctx, query, n, cursor)
	if err == nil && len(entries.Items) > 0 {
		entries.Items[0].Value["text"] = "adapter"
	}
	return entries, err
}
