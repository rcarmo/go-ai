package durable

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
)

import goai "github.com/rcarmo/go-ai"

// A decorator intentionally exposes only Storage. No nativeCore method crosses
// this boundary; Session must use the same public persistence seam as source.
type sourceStorageDecorator struct {
	Storage
	mints, commits, closes atomic.Int64
	refusal                error
	dropReceipts           bool
}

func (s *sourceStorageDecorator) MintID(ctx context.Context) (ID, error) {
	s.mints.Add(1)
	return s.Storage.MintID(ctx)
}
func (s *sourceStorageDecorator) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	s.commits.Add(1)
	if s.dropReceipts {
		return s.Storage.Commit(ctx, batches, func(int, uint64) {})
	}
	if s.refusal != nil {
		return CommitResult{Refused: NewRefusal(s.refusal)}, nil
	}
	return s.Storage.Commit(ctx, batches, receive)
}
func (s *sourceStorageDecorator) Close(ctx context.Context) error {
	s.closes.Add(1)
	return s.Storage.Close(ctx)
}

func TestSourceSessionHonoursPublicStorageDecorator(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		store := &sourceStorageDecorator{Storage: b.store}
		session, err := OpenSession(store)
		if err != nil {
			t.Fatal("source public Storage decorator rejected", err)
		}
		t.Cleanup(func() { _ = session.Close(bg) })
		value, err := session.Commit(bg, func(tx *Tx) (any, error) { return tx.CreateConversation(JSON{"ownership": JSON{"kind": "ownerless"}}) })
		if err != nil {
			t.Fatal(err)
		}
		conversation := value.(Conversation)
		value, err = session.Commit(bg, func(tx *Tx) (any, error) {
			return tx.AppendEntry(conversation.ID, JSON{"kind": "note", "data": JSON{"kept": true}})
		})
		if err != nil {
			t.Fatal(err)
		}
		entry := value.(Entry)
		stored, found, err := store.Entry(bg, entry.ID)
		if err != nil || !found || stored.Entry.Kind != "note" || !equalJSONValue(stored.Entry.Data, JSON{"kept": true}) {
			t.Fatal("public wrapped entry", stored, err)
		}
		if store.mints.Load() != 2 || store.commits.Load() != 2 {
			t.Fatal("Session bypassed public Storage methods", store.mints.Load(), store.commits.Load())
		}
		cause := errors.New("decorator refuses this write")
		store.refusal = cause
		_, err = session.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(conversation.ID, JSON{"kind": "must-not-store"}) })
		if !errors.Is(err, cause) {
			t.Fatal("decorator refusal lost", err)
		}
		if err := session.Close(bg); err != nil {
			t.Fatal(err)
		}
		if store.closes.Load() != 1 {
			t.Fatal("Session bypassed public Storage close", store.closes.Load())
		}
		// A refused prepared write fails/closes the Session in source. Inspect
		// durability through a reopened SQLite connection, never the closed one.
		reopened := utf16ReopenStore(t, b.store)
		entries, err := reopened.ScanEntries(bg, EntryQuery{Conversation: conversation.ID}, 10, nil)
		if err != nil || len(entries.Items) != 1 || entries.Items[0].ID != entry.ID {
			t.Fatal("decorator refusal was bypassed", entries, err)
		}
	})
}

func TestSourceHarnessUsesPublicStorageDecorator(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		store := &sourceStorageDecorator{Storage: b.store}
		var effects, requests atomic.Int64
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "wrapped", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "source.wrapped", Version: 1, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "wrapped result"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				events <- toolAnswer("wrapped-call", "wrapped", JSON{})
			} else {
				results := 0
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						results++
						if message.ToolCallID != "wrapped-call" || message.IsError || goai.ContentText(message.Content) != "wrapped result" {
							t.Error("wrapped model result", message)
						}
					}
				}
				if results != 1 {
					t.Error("wrapped result missing", results)
				}
				events <- terminal("answer")
			}
			close(events)
			return events
		})
		options.Registry = registry
		h := openHarness(t, store, options)
		conversation, err := h.Root(bg, AgentChange{Model: ref})
		if err != nil {
			t.Fatal(err)
		}
		submission, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, submission); result.Record.Status != "done" || effects.Load() != 1 || requests.Load() != 2 {
			wire, _ := json.Marshal(result.Record)
			t.Fatal("wrapped execution", string(wire), effects.Load(), requests.Load())
		}
		if store.mints.Load() == 0 || store.commits.Load() == 0 {
			t.Fatal("Harness bypassed public Storage", store.mints.Load(), store.commits.Load())
		}
		entries, err := store.ScanEntries(bg, EntryQuery{Conversation: conversation.ID()}, 20, nil)
		if err != nil || len(entries.Items) != 5 {
			kinds := []string{}
			for _, entry := range entries.Items {
				kinds = append(kinds, entry.Kind)
			}
			t.Fatal("wrapped durable transcript", len(entries.Items), kinds, err)
		}
	})
}

func TestPublicRefusalConstructorPreservesPresenceAndValue(t *testing.T) {
	cause := errors.New("original refusal")
	for _, value := range []any{nil, false, 0, "", cause} {
		refusal := NewRefusal(value)
		if refusal == nil || refusal.Value() != value || refusal.AsError() == nil {
			t.Fatal("refusal presence/value", value, refusal)
		}
		if value == cause && !errors.Is(refusal.AsError(), cause) {
			t.Fatal("refusal error identity")
		}
	}
}

// Concrete embedding must not accidentally grant the Session a bypass around
// an overriding public Commit method via a promoted private nativeCore method.
type sourceConcreteStorageDecorator struct {
	*SQLiteStorage
	commits atomic.Int64
}

func (s *sourceConcreteStorageDecorator) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	s.commits.Add(1)
	return s.SQLiteStorage.Commit(ctx, batches, receive)
}
func TestSourceSessionHonoursConcreteStorageDecorator(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		native, ok := b.store.(*SQLiteStorage)
		if !ok {
			t.Fatal("fixture must use the authorized SQLite engine")
		}
		store := &sourceConcreteStorageDecorator{SQLiteStorage: native}
		session, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		if _, err := session.Commit(bg, func(tx *Tx) (any, error) { return tx.CreateConversation(JSON{"ownership": JSON{"kind": "ownerless"}}) }); err != nil {
			t.Fatal(err)
		}
		if store.commits.Load() != 1 {
			t.Fatal("promoted nativeCore bypassed the decorator", store.commits.Load())
		}
	})
}

// Supplemental malformed-backend control: no source acceptance claim for a
// Storage implementation that violates its receipt-count contract.
func TestPublicStorageMissingReceiptDoesNotInventRollback(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		store := &sourceStorageDecorator{Storage: b.store}
		session, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		value, err := session.Commit(bg, func(tx *Tx) (any, error) { return tx.CreateConversation(JSON{"ownership": JSON{"kind": "ownerless"}}) })
		if err != nil {
			t.Fatal(err)
		}
		conversation := value.(Conversation)
		var observations atomic.Int64
		guard, err := session.ObserveCommits(CommitObserver{Advance: func(context.Context, CommitPublication) error { observations.Add(1); return nil }, End: func(WatchEnd) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Detach()
		store.dropReceipts = true
		var entryID ID
		_, err = session.Commit(bg, func(tx *Tx) (any, error) {
			entry, err := tx.AppendEntry(conversation.ID, JSON{"kind": "stored-without-receipt"})
			entryID = entry.ID
			return entry, err
		})
		if err == nil {
			t.Fatal("missing public receipt accepted")
		}
		if observations.Load() != 0 {
			t.Fatal("unconfirmed write published", observations.Load())
		}
		if err := session.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := utf16ReopenStore(t, b.store)
		stored, found, err := reopened.Entry(bg, entryID)
		if err != nil || !found || stored.Entry.Kind != "stored-without-receipt" {
			t.Fatal("unconfirmed outcome was falsely rolled back", stored, err)
		}
	})
}
