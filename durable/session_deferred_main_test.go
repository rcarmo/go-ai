package durable

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
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
		if err := startDetachedDocumentValues(watch, func(_ context.Context, value JSON, _ []Operation) error { values <- value; return nil }); err != nil {
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
		if _, err := s.Commit(bg, func(tx *Tx) (any, error) { return nil, mainDeferredSet(other, "unrelated")(tx) }); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
			h, err := tx.documentRecord(doc)
			if err != nil {
				return nil, err
			}
			_, err = h.Get()
			return nil, err
		}); err != nil {
			t.Fatal(err)
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Documents[doc].Value["text"] != "" {
			t.Fatal("unrelated/write-free commit flushed hold")
		}
		// A throwing writing callback leaves the hold and both stored docs alone.
		failure := errors.New("callback failure")
		if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
			h, err := tx.documentRecord(doc)
			if err != nil {
				return nil, err
			}
			if _, err = h.Get(); err != nil {
				return nil, err
			}
			if err = mainDeferredSet(other, "discarded")(tx); err != nil {
				return nil, err
			}
			return nil, failure
		}); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		if raw := mainDeferredSnapshot(t, b.store); raw.Documents[doc].Value["text"] != "" || raw.Documents[other].Value["text"] != "unrelated" {
			t.Fatal("failed callback changed storage")
		}
		// An actual writing commit that reads the document stores the held value.
		if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
			h, err := tx.documentRecord(doc)
			if err != nil {
				return nil, err
			}
			v, err := h.Get()
			if err != nil {
				return nil, err
			}
			if v["text"] != "ab" {
				return nil, errors.New("dependent read lost held value")
			}
			return nil, mainDeferredSet(other, "dependent")(tx)
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
		if _, err := s.Commit(bg, func(tx *Tx) (any, error) { return nil, mainDeferredAppend(doc, "d")(tx) }); err != nil {
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
			id, err := tx.mintID()
			if err != nil {
				return err
			}
			return tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "note"}})
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
			created, err = tx.mintID()
			if err != nil {
				return err
			}
			_, err = tx.createDocumentRecord(Document{ID: created, Scope: "conversation", Owner: 1, Kind: "test.created", Version: 1, History: "latest", Fork: "initial", Value: JSON{"text": "new"}})
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
				h, err := tx.documentRecord(doc)
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
		if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
			h, err := tx.documentRecord(doc)
			if err != nil {
				return nil, err
			}
			if _, err = h.Get(); err != nil {
				return nil, err
			}
			return nil, mainDeferredSet(other, "flush")(tx)
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
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
		// Native backends already own their reserved root conversation.
		cid := ID(1)
		var err error
		for _, p := range []*ID{&doc, &other} {
			*p, err = tx.mintID()
			if err != nil {
				return nil, err
			}
			kind := "test.text"
			if p == &other {
				kind = "test.other"
			}
			if _, err = tx.createDocumentRecord(Document{ID: *p, Scope: "conversation", Owner: cid, Kind: kind, Version: 1, History: "latest", Fork: "initial", Value: JSON{"text": ""}}); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, doc, other
}
func mainDeferredAppend(id ID, text string) func(*Tx) error {
	return func(tx *Tx) error {
		h, err := tx.documentRecord(id)
		if err != nil {
			return err
		}
		return h.ApplyOperations([]Operation{{"a", []any{"text"}, text}})
	}
}
func mainDeferredSet(id ID, text string) func(*Tx) error {
	return func(tx *Tx) error {
		h, err := tx.documentRecord(id)
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

// Source Session, Harness, Conversation, ToolExecutionApi and TaskRuntime
// commits all accept CommitOptions.persistence on the same public method.
func TestSourceCommitOptionsPublicMethodShapes(t *testing.T) {
	for _, receiver := range []any{(*Session)(nil), (*Harness)(nil), (*ConversationHandle)(nil), (*ToolAPI)(nil), (*TaskRuntime)(nil)} {
		typ := reflect.TypeOf(receiver)
		method, found := typ.MethodByName("Commit")
		if !found || !method.Type.IsVariadic() || method.Type.NumIn() != 4 {
			t.Error("source public commit options missing", typ)
			continue
		}
		options := method.Type.In(3).Elem()
		if options.Kind() != reflect.Struct || options.Name() != "CommitOptions" {
			t.Error("source CommitOptions mapping missing", typ, options)
			continue
		}
		persistence, found := options.FieldByName("Persistence")
		if !found || persistence.Type.Kind() != reflect.Pointer || persistence.Type.Elem().Name() != "CommitPersistence" {
			t.Error("optional CommitPersistence mapping missing", typ)
			continue
		}
		kind, found := persistence.Type.Elem().FieldByName("Kind")
		window, windowFound := persistence.Type.Elem().FieldByName("WithinMs")
		if !found || kind.Type.Kind() != reflect.String || !windowFound || window.Type.Kind() != reflect.Pointer || window.Type.Elem().Kind() != reflect.Float64 {
			t.Error("source persistence union/optional Number window missing", typ)
		}
	}
}

// Source deferred-commit policy through the actual public Session/Harness/
// conversation methods, without a private infinite-window entry.
func TestSourcePublicCommitPersistenceOptions(t *testing.T) {
	type committer interface {
		Commit(context.Context, func(*Tx) (any, error), ...CommitOptions) (any, error)
	}
	for _, name := range []string{"session", "harness", "conversation"} {
		t.Run(name, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			var session *Session
			var target any
			var closeOwner func() error
			if name == "session" {
				seedRootConversation(t, store)
				session, err = OpenSession(store)
				if err != nil {
					t.Fatal(err)
				}
				target = session
				closeOwner = func() error { return session.Close(bg) }
			} else {
				h := openHarness(t, store, Options{})
				conversation := root(t, h, ModelRef{Provider: "source", ID: "model"})
				session, target = h.session, h
				if name == "conversation" {
					target = conversation
				}
				closeOwner = func() error { return h.Close(bg) }
			}
			t.Cleanup(func() { closeOwner() })
			api, ok := target.(committer)
			if !ok {
				t.Fatal("public source CommitOptions signature missing", name)
			}
			definition, err := DefineDocument(DefinitionOptions{Kind: "source.options", Scope: "conversation", History: "latest", Fork: "initial", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err != nil {
				t.Fatal(err)
			}
			var docID ID
			if _, err := api.Commit(bg, func(tx *Tx) (any, error) {
				doc, err := tx.AcquireDocument(definition, 1, nil, nil)
				if err == nil {
					docID = doc.id
				}
				return nil, err
			}); err != nil {
				t.Fatal(err)
			}
			set := func(text string) func(*Tx) (any, error) {
				return func(tx *Tx) (any, error) {
					doc, err := tx.AcquireDocument(definition, 1, nil, nil)
					if err != nil {
						return nil, err
					}
					return text, doc.Set(JSON{"text": text})
				}
			}
			before, err := store.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			infinite := math.Inf(1)
			value, err := api.Commit(bg, set("held"), CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &infinite}})
			if err != nil || value != "held" {
				t.Fatal("public deferred commit", value, err)
			}
			raw, err := store.Snapshot(bg)
			if err != nil || raw.Seq != before.Seq || !equalJSONValue(raw.Documents[docID].Value, JSON{"text": ""}) {
				t.Error("deferred public commit stored early", raw.Seq, before.Seq, raw.Documents[docID].Value, err)
			}
			view, found, err := session.SnapshotDefinition(bg, definition, 1, nil)
			if err != nil || !found || !equalJSONValue(view.Copy(), JSON{"text": "held"}) {
				t.Error("public deferred memory publication missing", view, found, err)
			}
			value, err = api.Commit(bg, set("stored"), CommitOptions{Persistence: &CommitPersistence{Kind: "immediate"}})
			if err != nil || value != "stored" {
				t.Fatal(value, err)
			}
			raw, err = store.Snapshot(bg)
			if err != nil || raw.Seq != before.Seq+1 || !equalJSONValue(raw.Documents[docID].Value, JSON{"text": "stored"}) {
				t.Error("immediate dependency did not fold held/current values into one commit", raw.Seq, before.Seq, raw.Documents[docID].Value, err)
			}
			for _, invalid := range []float64{-1, math.NaN()} {
				called := false
				value, err := api.Commit(bg, func(*Tx) (any, error) { called = true; return "bad", nil }, CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &invalid}})
				if err == nil || value != nil || called {
					t.Error("invalid public window reached callback", value, err, called)
				}
			}
			invalid := math.NaN()
			for _, kind := range []string{"immediate", "unknown"} {
				value, err := api.Commit(bg, func(*Tx) (any, error) { return "immediate", nil }, CommitOptions{Persistence: &CommitPersistence{Kind: kind, WithinMs: &invalid}})
				if err != nil || value != "immediate" {
					t.Error("non-deferred policy incorrectly validated ignored window", kind, value, err)
				}
			}
			value, err = api.Commit(bg, func(*Tx) (any, error) { return "first option", nil }, CommitOptions{Persistence: &CommitPersistence{Kind: "immediate"}}, CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &invalid}})
			if err != nil || value != "first option" {
				t.Error("extra optional argument changed source first-option semantics", value, err)
			}
			beforeMixed := raw.Seq
			value, err = api.Commit(bg, func(tx *Tx) (any, error) {
				if _, err := set("mixed")(tx); err != nil {
					return nil, err
				}
				id, err := tx.mintID()
				if err != nil {
					return nil, err
				}
				return id, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "source.options-note", HasData: true, Data: JSON{"mixed": true}})
			}, CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &infinite}})
			entryID, validID := value.(ID)
			raw, readErr := store.Snapshot(bg)
			if err != nil || readErr != nil || !validID || entryID == 0 || raw.Seq != beforeMixed+1 || raw.Entries[entryID].Kind != "source.options-note" || !equalJSONValue(raw.Documents[docID].Value, JSON{"text": "mixed"}) {
				t.Error("mixed public transaction incorrectly deferred durable work", value, err, readErr, raw)
			}
			if err := closeOwner(); err != nil {
				t.Fatal(err)
			}
			called := false
			value, err = api.Commit(bg, func(*Tx) (any, error) { called = true; return "closed", nil }, CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &invalid}})
			if !errors.Is(err, ErrClosed) || value != nil || called {
				t.Error("closed must precede invalid persistence options", value, err, called)
			}
		})
	}
}

func TestSourceInvocationCommitPersistenceOptions(t *testing.T) {
	type toolCommitter interface {
		Commit(context.Context, func(*Tx) (any, error), ...CommitOptions) (any, error)
	}
	type runtimeCommitter interface {
		Commit(context.Context, func(*Tx, TaskRecord) (*TaskState, error), ...CommitOptions) error
	}
	for _, mode := range []string{"runtime", "tool"} {
		t.Run(mode, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			registry := NewRegistry()
			var h *Harness
			var docID ID
			definition, err := DefineDocument(DefinitionOptions{Kind: "source.invocation-options", Scope: "conversation", History: "latest", Fork: "initial", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err != nil {
				t.Fatal(err)
			}
			check := func(ctx context.Context, commit func(context.Context, func(*Tx) (any, error), ...CommitOptions) (any, error)) error {
				before, err := store.Snapshot(ctx)
				if err != nil {
					return err
				}
				set := func(text string) func(*Tx) (any, error) {
					return func(tx *Tx) (any, error) {
						doc, err := tx.AcquireDocument(definition, 1, nil, nil)
						if err != nil {
							return nil, err
						}
						return text, doc.Set(JSON{"text": text})
					}
				}
				infinite := math.Inf(1)
				value, err := commit(ctx, set("held"), CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &infinite}})
				if err != nil || value != "held" {
					t.Error("invocation deferred result", value, err)
					return err
				}
				raw, err := store.Snapshot(ctx)
				if err != nil || raw.Seq != before.Seq || !equalJSONValue(raw.Documents[docID].Value, JSON{"text": ""}) {
					t.Error("invocation deferred write stored early", raw.Seq, before.Seq, raw.Documents[docID].Value, err)
				}
				view, found, err := h.session.SnapshotDefinition(ctx, definition, 1, nil)
				if err != nil || !found || !equalJSONValue(view.Copy(), JSON{"text": "held"}) {
					t.Error("invocation memory publication missing", view, found, err)
				}
				value, err = commit(ctx, set("stored"), CommitOptions{Persistence: &CommitPersistence{Kind: "immediate"}})
				if err != nil || value != "stored" {
					t.Error("invocation immediate result", value, err)
					return err
				}
				raw, err = store.Snapshot(ctx)
				if err != nil || raw.Seq != before.Seq+1 || !equalJSONValue(raw.Documents[docID].Value, JSON{"text": "stored"}) {
					t.Error("invocation immediate dependency did not fold held/current values", raw.Seq, before.Seq, raw.Documents[docID].Value, err)
				}
				for _, invalid := range []float64{-1, math.NaN()} {
					called := false
					value, err := commit(ctx, func(*Tx) (any, error) { called = true; return "bad", nil }, CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &invalid}})
					if err == nil || value != nil || called {
						t.Error("invalid invocation options reached callback", value, err, called)
					}
				}
				return nil
			}
			reg := wrapRegistration("source_options_probe")
			reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
				committer, ok := any(api).(toolCommitter)
				if !ok {
					t.Error("source tool commit options missing")
					return ToolResult{IsError: true}, nil
				}
				err := check(ctx, committer.Commit)
				return ToolResult{Content: "stored"}, err
			}
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
			task := taskDefinition(t, "source.invocation-options", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if mode == "runtime" {
					committer, ok := any(r).(runtimeCommitter)
					if !ok {
						t.Error("source runtime commit options missing")
					} else if err := check(ctx, func(ctx context.Context, callback func(*Tx) (any, error), options ...CommitOptions) (any, error) {
						var value any
						err := committer.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
							var callbackErr error
							value, callbackErr = callback(tx)
							return nil, callbackErr
						}, options...)
						if err != nil {
							return nil, err
						}
						return value, nil
					}); err != nil {
						return err
					}
				} else {
					result, err := r.CallTool(ctx, "options", "source_options_probe", JSON{})
					if err != nil || result.IsError || result.Content != "stored" {
						t.Error("source tool options call", result, err)
					}
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			if _, err := registry.RegisterTask(task); err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, store, Options{Registry: registry})
			conversation := root(t, h, ModelRef{Provider: "source", ID: "model"})
			if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
				doc, err := tx.AcquireDocument(definition, conversation.ID(), nil, nil)
				if err == nil {
					docID = doc.id
				}
				return nil, err
			}); err != nil {
				t.Fatal(err)
			}
			record := waitPublicTask(t, h, createPublicTask(t, h, task, nil, TaskOptions{Conversation: conversation.ID(), Ownership: TaskOwnership{Kind: "conversation"}}))
			if record.State.Outcome.Status != "completed" {
				t.Error("invocation options task", record.State.Outcome)
			}
		})
	}
}

// Source #deferral reads the explicit/default Number at the call boundary,
// before a queued callback can start; later options/settings changes cannot win.
func TestSourceCommitPersistenceWindowCapturedBeforeAdmission(t *testing.T) {
	for _, mode := range []string{"explicit", "default"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				window := math.Inf(1)
				f := openMainDeadlineSession(t, b, &window)
				before := mainDeferredSnapshot(t, b.store)
				entered, release := make(chan struct{}), make(chan struct{})
				defer releaseTaskGate(release)
				blocked := make(chan error, 1)
				go func() {
					_, err := f.session.Commit(bg, func(*Tx) (any, error) { close(entered); <-release; return nil, nil })
					blocked <- err
				}()
				watchReceive(t, entered)
				admission := &mainCommitAdmissionContext{Context: bg, entered: make(chan struct{})}
				persistence := &CommitPersistence{Kind: "deferred"}
				if mode == "explicit" {
					persistence.WithinMs = &window
				}
				settled := make(chan error, 1)
				go func() {
					_, err := f.session.Commit(admission, func(tx *Tx) (any, error) { return "held", mainDeferredAppend(f.doc, "held")(tx) }, CommitOptions{Persistence: persistence})
					settled <- err
				}()
				watchReceive(t, admission.entered)
				window = 0
				releaseTaskGate(release)
				if err := watchReceive(t, blocked); err != nil {
					t.Fatal(err)
				}
				if err := watchReceive(t, settled); err != nil {
					t.Fatal(err)
				}
				f.assertArmed(t)
				raw := mainDeferredSnapshot(t, b.store)
				if raw.Seq != before.Seq || !equalJSONValue(raw.Documents[f.doc].Value, before.Documents[f.doc].Value) {
					t.Error("queued commit read later persistence policy", raw, before)
				}
			})
		})
	}
}

func TestSourcePublicCommitNumericWindows(t *testing.T) {
	for _, test := range []struct {
		name   string
		window float64
		delay  time.Duration
	}{{"zero", 0, 0}, {"fraction", .25, 250 * time.Microsecond}, {"infinite", math.Inf(1), 0}} {
		t.Run(test.name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := openMainDeadlineSession(t, b, nil)
				before := mainDeferredSnapshot(t, b.store).Seq
				value, err := f.session.Commit(bg, func(tx *Tx) (any, error) { return "held", mainDeferredAppend(f.doc, "held")(tx) }, CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &test.window}})
				if err != nil || value != "held" {
					t.Fatal(value, err)
				}
				if math.IsInf(test.window, 1) {
					f.assertArmed(t)
				} else {
					f.assertArmed(t, test.delay)
				}
				if raw := mainDeferredSnapshot(t, b.store); raw.Seq != before {
					t.Error("public numeric window stored early", raw.Seq, before)
				}
				if !math.IsInf(test.window, 1) {
					f.fire(t)
					if raw := mainDeferredSnapshot(t, b.store); raw.Seq != before+1 || !equalJSONValue(raw.Documents[f.doc].Value, JSON{"text": "held"}) {
						t.Error("public numeric deadline lost hold", raw)
					}
				}
			})
		})
	}
}

func TestSourceInvalidCommitOptionsRejectBeforeAdmission(t *testing.T) {
	s := gatedSession(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer releaseTaskGate(release)
	blocked := make(chan error, 1)
	go func() {
		_, err := s.Commit(bg, func(*Tx) (any, error) { close(entered); <-release; return nil, nil })
		blocked <- err
	}()
	watchReceive(t, entered)
	invalid := math.NaN()
	type result struct {
		value  any
		err    error
		called bool
	}
	settled := make(chan result, 1)
	go func() {
		called := false
		value, err := s.Commit(bg, func(*Tx) (any, error) { called = true; return "bad", nil }, CommitOptions{Persistence: &CommitPersistence{Kind: "deferred", WithinMs: &invalid}})
		settled <- result{value, err, called}
	}()
	outcome := watchReceive(t, settled)
	if outcome.err == nil || outcome.value != nil || outcome.called {
		t.Error("invalid options queued behind admission or ran callback", outcome)
	}
	releaseTaskGate(release)
	if err := watchReceive(t, blocked); err != nil {
		t.Fatal(err)
	}
}
