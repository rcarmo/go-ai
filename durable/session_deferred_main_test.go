package durable

import (
	"context"
	"errors"
	"testing"
)

// Source-derived scenarios from session-deferred-commits.test.ts:189,285,299,
// 316,326,391,410,521; complete source bodies remain unported. Native adaptations
// use the reserved root, raw document handles, an internal infinite-window entry
// and channel observation of asynchronous watches. Backend writes are real.
func TestMainSessionDeferredInfiniteWindow(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, other := openMainDeferredSession(t, b)
		baseline := mainDeferredSnapshot(t, b.store).Seq
		_, stored, err := s.SubscribeCommits(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer stored.Stop()
		def, err := DefineDocument(DefinitionOptions{Kind: "test.text", Version: 1, Scope: "conversation", History: "latest", Fork: "initial", Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
		if err != nil {
			t.Fatal(err)
		}
		watch, ok, err := s.WatchDefinition(bg, def, 1, nil)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		defer watch.Stop()
		values := make(chan JSON, 8)
		if err := watch.Start(func(_ context.Context, value JSON, _ []Operation) error { values <- value; return nil }); err != nil {
			t.Fatal(err)
		}
		for index, text := range []string{"a", "ab"} {
			part := []string{"a", "b"}[index]
			if _, err := s.commitDeferred(bg, mainDeferredAppend(doc, part)); err != nil {
				t.Fatal(err)
			}
			memory, err := s.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if memory.Documents[doc].Value["text"] != text || memory.Seq != baseline {
				t.Fatal("memory/durable sequence", memory.Seq, memory.Documents[doc])
			}
			memory.Documents[doc].Value["text"] = "caller mutation"
			value := watchReceive(t, values)
			if value["text"] != text {
				t.Fatal("watch missed held change", value)
			}
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Seq != baseline || raw.Documents[doc].Value["text"] != "" {
			t.Fatal("held value was stored", raw)
		}
		stored.mu.Lock()
		count := len(stored.pending)
		stored.mu.Unlock()
		if count != 0 {
			t.Fatal("stored observer received memory publication", count)
		}
		// Neither an unrelated write nor a write-free dependent read stores it.
		if _, err := s.Commit(bg, mainDeferredSet(other, "unrelated")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit(bg, func(tx *Tx) error {
			h, err := tx.Document(doc)
			if err != nil {
				return err
			}
			_, err = h.Get()
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Documents[doc].Value["text"] != "" {
			t.Fatal("unrelated/write-free commit flushed hold")
		}
		// A throwing writing callback leaves the hold and both stored docs alone.
		failure := errors.New("callback failure")
		if _, err := s.Commit(bg, func(tx *Tx) error {
			h, err := tx.Document(doc)
			if err != nil {
				return err
			}
			if _, err = h.Get(); err != nil {
				return err
			}
			if err = mainDeferredSet(other, "discarded")(tx); err != nil {
				return err
			}
			return failure
		}); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Documents[doc].Value["text"] != "" || raw.Documents[other].Value["text"] != "unrelated" {
			t.Fatal("failed callback changed storage")
		}
		// An actual writing commit that reads the document stores the held value.
		if _, err := s.Commit(bg, func(tx *Tx) error {
			h, err := tx.Document(doc)
			if err != nil {
				return err
			}
			v, err := h.Get()
			if err != nil {
				return err
			}
			if v["text"] != "ab" {
				return errors.New("dependent read lost held value")
			}
			return mainDeferredSet(other, "dependent")(tx)
		}); err != nil {
			t.Fatal(err)
		}
		raw := mainDeferredSnapshot(t, b.store)
		if raw.Documents[doc].Value["text"] != "ab" || raw.Documents[doc].DeltasSinceBase != 1 {
			t.Fatal("dependent fold", raw.Documents[doc])
		}
		if _, err := s.commitDeferred(bg, mainDeferredAppend(doc, "c")); err != nil {
			t.Fatal(err)
		}
		if value := watchReceive(t, values); value["text"] != "abc" {
			t.Fatal("duplicate memory delivery at dependent flush", value)
		}
		if _, err := s.Commit(bg, mainDeferredAppend(doc, "d")); err != nil {
			t.Fatal(err)
		}
		if value := watchReceive(t, values); value["text"] != "abcd" {
			t.Fatal("final memory value", value)
		}
		raw = mainDeferredSnapshot(t, b.store)
		if raw.Documents[doc].Value["text"] != "abcd" || raw.Documents[doc].DeltasSinceBase != 2 {
			t.Fatal("dependent write was not one folded delta", raw.Documents[doc])
		}
		stored.mu.Lock()
		publications := append([]PublicationFrame{}, stored.pending...)
		stored.mu.Unlock()
		if len(publications) != 3 {
			t.Fatal("stored observer batch count", len(publications))
		}
		replica := map[ID]JSON{doc: {"text": ""}, other: {"text": ""}}
		for index, frame := range publications {
			if frame.Publication.Seq != baseline+uint64(index)+1 {
				t.Fatal("stored observer sequence", frame.Publication.Seq)
			}
			for _, change := range frame.Publication.Documents {
				value, err := ApplyOperations(replica[change.Record.ID], change.Ops, s.limits)
				if err != nil || !equalJSONValue(value, change.Value) {
					t.Fatal("stored operation replay", value, change.Value, err)
				}
				replica[change.Record.ID] = change.Value
			}
		}
		if !equalJSONValue(replica[doc], raw.Documents[doc].Value) || !equalJSONValue(replica[other], raw.Documents[other].Value) {
			t.Fatal("stored observer replica differs")
		}
	})
}

func TestMainSessionDeferredFallbackAndClose(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, other := openMainDeferredSession(t, b)
		// Mixed document/entry writes are immediately durable, not eligible holds.
		if _, err := s.commitDeferred(bg, func(tx *Tx) error {
			if err := mainDeferredSet(doc, "mixed")(tx); err != nil {
				return err
			}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "note"}})
		}); err != nil {
			t.Fatal(err)
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Documents[doc].Value["text"] != "mixed" {
			t.Fatal("mixed writes deferred")
		}
		if _, err := s.commitDeferred(bg, mainDeferredSet(doc, "close")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.commitDeferred(bg, mainDeferredSet(other, "peer")); err != nil {
			t.Fatal(err)
		}
		before := mainDeferredSnapshot(t, b.store).Seq
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := utf16ReopenStore(t, b.store)
		defer reopened.Close(bg)
		raw := mainDeferredSnapshot(t, reopened)
		if raw.Seq != before+1 || raw.Documents[doc].Value["text"] != "close" || raw.Documents[other].Value["text"] != "peer" {
			t.Fatal("Close did not store holds together", raw.Seq, before)
		}
	})
}

func TestMainSessionDeferredFailureDoesNotFlush(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, _ := openMainDeferredSession(t, b)
		if _, err := s.commitDeferred(bg, mainDeferredSet(doc, "not durable")); err != nil {
			t.Fatal(err)
		}
		before := mainDeferredSnapshot(t, b.store).Seq
		_ = s.backendResult(bg, errors.New("simulated Session failure"), true)
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := utf16ReopenStore(t, b.store)
		defer reopened.Close(bg)
		raw := mainDeferredSnapshot(t, reopened)
		if raw.Seq != before || raw.Documents[doc].Value["text"] != "" {
			t.Fatal("failure flushed volatile state", raw)
		}
	})
}

// Source-derived control: creation remains immediate even with an unrelated
// hold, and the stored publication must include the callback's minted IDs.
func TestMainSessionDeferredCreationIsImmediate(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, _ := openMainDeferredSession(t, b)
		if _, err := s.commitDeferred(bg, mainDeferredAppend(doc, "a")); err != nil {
			t.Fatal(err)
		}
		before := mainDeferredSnapshot(t, b.store).Seq
		var created ID
		if _, err := s.commitDeferred(bg, func(tx *Tx) error {
			var err error
			created, err = tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: created, Scope: "conversation", Owner: 1, Kind: "test.created", Version: 1, History: "latest", Fork: "initial", Value: JSON{"text": "new"}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		raw := mainDeferredSnapshot(t, b.store)
		if raw.Seq != before+1 || raw.Documents[created].Value["text"] != "new" || raw.Documents[doc].Value["text"] != "" {
			t.Fatal("creation/hold storage boundary", raw.Seq, before, raw.Documents)
		}
		memory, err := s.Snapshot(bg)
		if err != nil || memory.Documents[doc].Value["text"] != "a" {
			t.Fatal("creation discarded unrelated hold", err)
		}
	})
}

// Native-budget control, not a port of the source's 10,000-operation boundary.
// Non-primitive sets are folding barriers, so the tail really exceeds 4,096.
func TestMainSessionDeferredOperationOverflowStoresBase(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, other := openMainDeferredSession(t, b)
		total := 0
		for _, count := range []int{maxDeltaOperations / 2, maxDeltaOperations / 2, 1} {
			ops := make([]Operation, count)
			for index := range ops {
				total++
				ops[index] = Operation{"s", []any{"box"}, map[string]any{"n": total}}
			}
			if _, err := s.commitDeferred(bg, func(tx *Tx) error {
				h, err := tx.Document(doc)
				if err != nil {
					return err
				}
				return h.ApplyOperations(ops)
			}); err != nil {
				t.Fatal(err)
			}
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Documents[doc].Value["box"] != nil {
			t.Fatal("overflow caused an early store")
		}
		if _, err := s.Commit(bg, func(tx *Tx) error {
			h, err := tx.Document(doc)
			if err != nil {
				return err
			}
			if _, err = h.Get(); err != nil {
				return err
			}
			return mainDeferredSet(other, "flush")(tx)
		}); err != nil {
			t.Fatal(err)
		}
		raw := mainDeferredSnapshot(t, b.store)
		if raw.Documents[doc].DeltasSinceBase != 0 || !equalJSONValue(raw.Documents[doc].Value["box"], map[string]any{"n": total}) {
			t.Fatal("overflow did not store exact final base", raw.Documents[doc])
		}
	})
}

func TestMainSessionDeferredCloseRefusalStillCloses(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, _ := openMainDeferredSession(t, b)
		if _, err := s.commitDeferred(bg, mainDeferredAppend(doc, "held")); err != nil {
			t.Fatal(err)
		}
		before := mainDeferredSnapshot(t, b.store).Seq
		refusal := &StorageRejected{Reason: "close flush refused"}
		s.taskBookkeeping(func() {
			if err := s.core.enter(bg); err != nil {
				t.Fatal(err)
			}
			defer s.core.leave()
			original := s.core.appendFrame
			s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				if kind == 2 {
					return refusal
				}
				if original != nil {
					return original(kind, ordinal, high, payload)
				}
				return nil
			}
		})
		if err := s.Close(bg); err != refusal {
			t.Fatal("lost close flush failure identity", err)
		}
		// Remaining c9 source391 assertions: failed end retains exact cause,
		// and the original backend rejects calls until it is reopened.
		if end, err := s.Closed(bg); err != nil || end.Reason != "failed" || end.Error != refusal {
			t.Fatal("close refusal end/cause", end, err)
		}
		if _, err := b.store.Snapshot(bg); !errors.Is(err, ErrClosed) {
			t.Fatal("close refusal left backend usable", err)
		}
		select {
		case <-s.core.closeDone:
		default:
			t.Fatal("flush failure skipped Storage close")
		}
		reopened := utf16ReopenStore(t, b.store)
		defer reopened.Close(bg)
		raw := mainDeferredSnapshot(t, reopened)
		if raw.Seq != before || raw.Documents[doc].Value["text"] != "" {
			t.Fatal("refused hold was stored")
		}
	})
}

func TestMainSessionDeferredCancellationAndAdapterAdmission(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, doc, _ := openMainDeferredSession(t, b)
		if _, err := s.commitDeferred(bg, mainDeferredAppend(doc, "a")); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(bg)
		_, err := s.commitDeferred(ctx, func(tx *Tx) error {
			if err := mainDeferredAppend(doc, "discard")(tx); err != nil {
				return err
			}
			cancel()
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		memory, err := s.Snapshot(bg)
		if err != nil || memory.Documents[doc].Value["text"] != "a" {
			t.Fatal("cancel changed held value", err)
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Documents[doc].Value["text"] != "" {
			t.Fatal("cancel persisted hold")
		}
		// This initial internal boundary cannot bypass an adapter's authority.
		s.taskBookkeeping(func() { s.store = &mainDeferredAdapter{Storage: s.store} })
		called := false
		if _, err := s.commitDeferred(bg, func(*Tx) error { called = true; return nil }); !errors.Is(err, ErrUnsupported) || called {
			t.Fatal("adapter admitted deferred callback", err, called)
		}
		s.taskBookkeeping(func() { s.store = b.store })
	})
}

type mainDeferredAdapter struct{ Storage }

func (a *mainDeferredAdapter) nativeCore() *storeCore { return a.Storage.(nativeStorage).nativeCore() }

func openMainDeferredSession(t *testing.T, b sqliteFixture) (*Session, ID, ID) {
	t.Helper()
	seedRootConversation(t, b.store)
	s, err := OpenSession(b.store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-s.done:
			return
		default:
		}
		if err := s.Close(bg); err != nil {
			t.Error(err)
		}
	})
	var doc, other ID
	if _, err := s.Commit(bg, func(tx *Tx) error {
		// Native backends already own their reserved root conversation.
		cid := ID(1)
		var err error
		for _, p := range []*ID{&doc, &other} {
			*p, err = tx.MintID()
			if err != nil {
				return err
			}
			kind := "test.text"
			if p == &other {
				kind = "test.other"
			}
			if _, err = tx.CreateDocument(Document{ID: *p, Scope: "conversation", Owner: cid, Kind: kind, Version: 1, History: "latest", Fork: "initial", Value: JSON{"text": ""}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, doc, other
}
func mainDeferredAppend(id ID, text string) func(*Tx) error {
	return func(tx *Tx) error {
		h, err := tx.Document(id)
		if err != nil {
			return err
		}
		return h.ApplyOperations([]Operation{{"a", []any{"text"}, text}})
	}
}
func mainDeferredSet(id ID, text string) func(*Tx) error {
	return func(tx *Tx) error {
		h, err := tx.Document(id)
		if err != nil {
			return err
		}
		return h.Set(JSON{"text": text})
	}
}
func mainDeferredSnapshot(t *testing.T, store Storage) Snapshot {
	t.Helper()
	v, err := store.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
