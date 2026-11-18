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
	rejectAfter            error
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
	result, err := s.Storage.Commit(ctx, batches, receive)
	if err == nil && s.rejectAfter != nil {
		err = s.rejectAfter
	}
	return result, err
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

// Selected source group-commit assertion: delivered receipts are published even
// when the storage call rejects afterwards, and that call still rejects its job.
func TestSourceDecoratedReceiptPublishesBeforeBackendFailure(t *testing.T) {
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
		cause := errors.New("connection lost after receipt")
		store.rejectAfter = cause
		var entryID ID
		_, err = session.Commit(bg, func(tx *Tx) (any, error) {
			entry, err := tx.AppendEntry(conversation.ID, JSON{"kind": "confirmed-before-error"})
			entryID = entry.ID
			return entry, err
		})
		if !errors.Is(err, cause) {
			t.Fatal("received storage call lost its own failure", err)
		}
		if observations.Load() != 1 {
			t.Fatal("confirmed prefix was not published", observations.Load())
		}
		if err := session.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := utf16ReopenStore(t, b.store)
		stored, found, err := reopened.Entry(bg, entryID)
		if err != nil || !found || stored.Entry.Kind != "confirmed-before-error" {
			t.Fatal("confirmed prefix did not survive reopen", stored, err)
		}
	})
}

type sourcePrefixStorageDecorator struct {
	Storage
	hold             atomic.Bool
	entered, release chan struct{}
	cause            error
	sizes            chan int
}

func (s *sourcePrefixStorageDecorator) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	s.sizes <- len(batches)
	if s.hold.CompareAndSwap(true, false) {
		close(s.entered)
		<-s.release
	}
	for index, batch := range batches {
		for _, command := range batch {
			if command.Type == "entry" {
				entry, ok := command.Value.(Entry)
				if ok && entry.Kind == "refused" {
					result, err := s.Storage.Commit(ctx, batches[:index], receive)
					if err != nil {
						return result, err
					}
					result.Refused = NewRefusal(s.cause)
					return result, nil
				}
			}
		}
	}
	return s.Storage.Commit(ctx, batches, receive)
}

// Port of the source grouped-prefix/own-refusal ordering scenario, driven only
// through public Session callbacks and the public Storage commit decorator.
func TestSourceDecoratedGroupedPrefixRefusalAndOwnOutcomes(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		cause := errors.New("refused last grouped part")
		store := &sourcePrefixStorageDecorator{Storage: sourceGappedStorage{b.store.(*SQLiteStorage)}, entered: make(chan struct{}), release: make(chan struct{}), cause: cause, sizes: make(chan int, 8)}
		session, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { releaseTaskGate(store.release); _ = session.Close(bg) }()
		value, err := session.Commit(bg, func(tx *Tx) (any, error) { return tx.CreateConversation(JSON{"ownership": JSON{"kind": "ownerless"}}) })
		if err != nil {
			t.Fatal(err)
		}
		conversation := value.(Conversation)
		watchReceive(t, store.sizes)
		var observations atomic.Int64
		var seqs [3]uint64
		guard, err := session.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
			if index := observations.Load(); index < int64(len(seqs)) {
				seqs[index] = p.Seq
			}
			observations.Add(1)
			return nil
		}, End: func(WatchEnd) error { return nil }})
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Detach()
		type outcome struct {
			value any
			err   error
		}
		appendEntry := func(kind string, entered chan struct{}) <-chan outcome {
			done := make(chan outcome, 1)
			go func() {
				value, err := session.Commit(bg, func(tx *Tx) (any, error) {
					if entered != nil {
						close(entered)
					}
					return tx.AppendEntry(conversation.ID, JSON{"kind": kind})
				})
				done <- outcome{value, err}
			}()
			return done
		}
		store.hold.Store(true)
		first := appendEntry("first", nil)
		awaitTaskSignal(t, store.entered)
		secondEntered := make(chan struct{})
		second := appendEntry("second", secondEntered)
		awaitTaskSignal(t, secondEntered)
		thirdEntered := make(chan struct{})
		third := appendEntry("third", thirdEntered)
		awaitTaskSignal(t, thirdEntered)
		refusedEntered := make(chan struct{})
		refused := appendEntry("refused", refusedEntered)
		awaitTaskSignal(t, refusedEntered)
		// The next callback's admission witnesses that the refused callback has
		// returned and its part was queued; no scheduling sleeps or polling.
		noopEntered := make(chan struct{})
		noop := make(chan error, 1)
		go func() {
			_, err := session.Commit(bg, func(*Tx) (any, error) { close(noopEntered); return nil, nil })
			noop <- err
		}()
		awaitTaskSignal(t, noopEntered)
		releaseTaskGate(store.release)
		if result := watchReceive(t, first); result.err != nil {
			t.Fatal(result.err)
		}
		if result := watchReceive(t, second); result.err != nil {
			t.Fatal("durable prefix lost its outcome", result.err)
		}
		if result := watchReceive(t, third); result.err != nil {
			t.Fatal("second durable grouped part lost its outcome", result.err)
		}
		if result := watchReceive(t, refused); !errors.Is(result.err, cause) {
			t.Fatal("refused part lost its own cause", result.err)
		}
		var failed *SessionFailed
		if err := watchReceive(t, noop); !errors.As(err, &failed) || !errors.Is(err, cause) {
			t.Fatal("later no-write outcome must be SessionFailed", err)
		}
		if firstSize, secondSize := watchReceive(t, store.sizes), watchReceive(t, store.sizes); firstSize != 1 || secondSize != 3 {
			t.Fatal("expected actual public grouped Storage call", firstSize, secondSize)
		}
		if observations.Load() != 3 {
			t.Fatal("must publish only the durable prefix", observations.Load())
		}
		if err := session.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := utf16ReopenStore(t, b.store)
		entries, err := reopened.ScanEntries(bg, EntryQuery{Conversation: conversation.ID}, 10, nil)
		if err != nil || len(entries.Items) != 3 || entries.Items[0].Kind != "third" || entries.Items[1].Kind != "second" || entries.Items[2].Kind != "first" {
			t.Fatal("wrong durable grouped prefix", entries, err)
		}
		for index, entry := range entries.Items {
			stored, found, err := reopened.Entry(bg, entry.ID)
			if err != nil || !found || stored.CommitSeq != seqs[2-index] {
				t.Fatalf("prefix part %s: published %v, stored %+v, found %v, err %v", entry.Kind, seqs, stored, found, err)
			}
		}
		if seqs[0]+1 >= seqs[1] || seqs[1]+1 != seqs[2] {
			t.Fatalf("expected a gap before the grouped prefix, then one seq per part: %v", seqs)
		}
	})
}

// SQLite's empty native transaction stands in for a gap in an otherwise
// ordinary Storage sequence; no extra public batch or visible row is created.
type sourceGappedStorage struct{ *SQLiteStorage }

func (s sourceGappedStorage) Commit(ctx context.Context, writes [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	if _, err := s.SQLiteStorage.Apply(ctx, Batch{Writes: []Write{}}); err != nil {
		return CommitResult{}, err
	}
	return s.SQLiteStorage.Commit(ctx, writes, receive)
}

func TestSourcePublicStorageReceiptSequenceIsAuthoritative(t *testing.T) {
	bg := context.Background()
	sqliteStore(t, func(t *testing.T, fixture sqliteFixture) {
		store := fixture.store.(*SQLiteStorage)
		before, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		session, err := OpenSession(sourceGappedStorage{store})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close(bg) })
		observed := make(chan uint64, 2)
		_, err = session.ObserveCommits(CommitObserver{
			Advance: func(_ context.Context, p CommitPublication) error { observed <- p.Seq; return nil },
			End:     func(WatchEnd) error { return nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		for i := uint64(1); i <= 2; i++ {
			value, err := session.Commit(bg, func(tx *Tx) (any, error) {
				return tx.AppendEntry(1, EntryDraft{"kind": "gapped"})
			})
			if err != nil {
				t.Fatalf("Session rejected actual storage receipt: %v", err)
			}
			stored, found, err := store.Entry(bg, value.(Entry).ID)
			want := before.Seq + 2*i
			if err != nil || !found || stored.CommitSeq != want {
				t.Fatalf("stored entry: %+v, %v, %v; want seq %d", stored, found, err, want)
			}
			select {
			case got := <-observed:
				if got != stored.CommitSeq {
					t.Fatalf("published seq %d; stored seq %d", got, stored.CommitSeq)
				}
			default:
				t.Fatal("durable part not published before its caller resumed")
			}
		}
	})
}
