package durable

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Consolidated from session_cached_snapshot_v150_test.go.

// session.ts snapshot: a same-version cached value is returned without enqueue.
// It is the last adopted value, not the draft of a pending writer. The cached
// branch does not pass its context to Storage, so cancellation is not consulted.
func TestSessionV150CachedSnapshotBypassesAdmissionAndCancellation(t *testing.T) {
	s := gatedSession(t)
	def, err := DefineDocument(DefinitionOptions{Kind: "snapshot.cached", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(def, 0, nil, nil); return nil, err }); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	commit := s.writer.host.commit
	s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		close(entered)
		<-release
		return commit(ctx, batches, receive)
	}
	stored := make(chan error, 1)
	go func() {
		_, err := s.Commit(bg, func(tx *Tx) (any, error) {
			doc, err := tx.AcquireDocument(def, 0, nil, nil)
			if err != nil {
				return nil, err
			}
			return nil, doc.Set(JSON{"n": 1})
		})
		stored <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(bg)
	cancel()
	beforeSlots := len(s.outcomeSlots)
	returned := make(chan error, 1)
	go func() {
		value, found, err := detachedDefinitionSnapshot(s, ctx, def, 0, nil)
		if err == nil {
			n, ok := exactNumber(value["n"])
			if !found || !ok || n == nil || n.Sign() != 0 {
				err = reject("cached snapshot exposed pending draft")
			}
		}
		returned <- err
	}()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cached snapshot entered the writer fence")
	}
	if len(s.outcomeSlots) != beforeSlots {
		t.Fatal("cached snapshot registered an ordinary outcome")
	}
	unblock()
	if err := <-stored; err != nil {
		t.Fatal(err)
	}
	value, found, err := detachedDefinitionSnapshot(s, bg, def, 0, nil)
	n, ok := exactNumber(value["n"])
	if err != nil || !found || !ok || n == nil || n.Num().Int64() != 1 {
		t.Fatal("cache did not advance at adoption", value, found, err)
	}
}

func TestSessionV150CacheVersionIsSharedAcrossDefinitionTokens(t *testing.T) {
	s := gatedSession(t)
	options := DefinitionOptions{Kind: "cache.version", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }}
	old, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(old, 0, nil, nil); return nil, err }); err != nil {
		t.Fatal(err)
	}
	first, second := 0, 0
	options.Version = 2
	options.Migrate = func(JSON, uint64) (JSON, error) { first++; return JSON{"n": 10}, nil }
	a, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	options.Migrate = func(JSON, uint64) (JSON, error) { second++; return JSON{"n": 99}, nil }
	b, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range []*DocumentDefinition{a, b} {
		value, found, err := detachedDefinitionSnapshot(s, bg, def, 0, nil)
		n, ok := exactNumber(value["n"])
		if err != nil || !found || !ok || n == nil || n.Num().Int64() != 10 {
			t.Fatal("same-version token did not share materialized value", value, found, err)
		}
	}
	if first != 1 || second != 0 {
		t.Fatal("cache was keyed by token identity", first, second)
	}
}

// Consolidated from document_retention_120_test.go.

func TestDocument120IdleRetentionZeroExpiryAndWatchPin(t *testing.T) {
	for _, mode := range []string{"zero", "expiry", "pinned"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				var calls atomic.Int64
				retention := int64(20)
				if mode == "zero" {
					retention = 0
				}
				s, e := OpenSessionWithOptions(b.store, SessionOptions{DocumentRetentionMs: &retention})
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close(bg)
				def, e := DefineDocument(DefinitionOptions{CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }, Kind: "retained", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"x": 0}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { calls.Add(1); return v, nil }})
				if e != nil {
					t.Fatal(e)
				}
				_, e = s.Commit(bg, func(tx *Tx) (any, error) {
					id, e := tx.mintID()
					if e != nil {
						return nil, e
					}
					_, e = tx.createDocumentRecord(Document{ID: id, Scope: "session", Kind: "retained", Version: 1, Value: JSON{"x": 1}})
					return nil, e
				})
				if e != nil {
					t.Fatal(e)
				}
				var watch *DocumentWatch
				if mode == "pinned" {
					watch, _, e = s.WatchDefinition(bg, def, 0, nil)
					if e != nil {
						t.Fatal(e)
					}
					defer watch.Stop()
				}
				read := func() {
					v, ok, e := detachedDefinitionSnapshot(s, bg, def, 0, nil)
					if e != nil || !ok || v["x"] != json.Number("1") {
						t.Fatal(v, ok, e)
					}
				}
				read()
				read()
				if mode == "zero" {
					if calls.Load() != 2 {
						t.Fatal("zero retention cached", calls.Load())
					}
					return
				}
				if calls.Load() != 1 {
					t.Fatal("warm migration reran", calls.Load())
				}
				if mode == "pinned" {
					time.Sleep(60 * time.Millisecond)
					read()
					if calls.Load() != 1 {
						t.Fatal("attached value expired", calls.Load())
					}
					watch.Stop()
				}
				deadline, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					empty := false
					s.taskBookkeeping(func() { empty = len(s.definitionCache) == 0 })
					if empty {
						break
					}
					select {
					case <-deadline.Done():
						t.Fatal("idle document timer did not evict")
					case <-time.After(time.Millisecond):
					}
				}
				read()
				if calls.Load() != 2 {
					t.Fatal("expired migration not recomputed", calls.Load())
				}
			})
		})
	}
}

// Source-derived snapshot()/observeDoc() shared-tracker contract, independently
// exercised against the pinned implementation by the source oracle runner.
func TestSessionV150SnapshotAndObserverShareReadonlyRevision(t *testing.T) {
	s := gatedSession(t)
	def, err := DefineDocument(DefinitionOptions{Kind: "oracle.readonly.snapshot", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0, "keep": JSON{"text": "original"}}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(def, 0, nil, nil); return nil, err }); err != nil {
		t.Fatal(err)
	}
	raw, found, err := s.SnapshotDefinition(bg, def, 0, nil)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	first, ok := any(raw).(ReadonlyValue)
	if !ok {
		t.Fatal("public snapshot is a mutable copy rather than the shared readonly tracker")
	}
	again, _, err := s.SnapshotDefinition(bg, def, 0, nil)
	if err != nil || any(again) != first {
		t.Fatal("same-version snapshots lost identity", err)
	}
	var frames []DocumentCall
	observation, found, err := s.ObserveDefinition(bg, def, 0, nil, DocumentObserver{Advance: func(_ context.Context, call DocumentCall) error { frames = append(frames, call); return nil }, End: func(WatchEnd) error { return nil }})
	if err != nil || !found {
		t.Fatal(found, err)
	}
	defer observation.Detach()
	if observation.Value != first {
		t.Fatal("observer did not attach to the snapshot's tracker")
	}
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(def, 0, nil, nil)
		if err != nil {
			return nil, err
		}
		return nil, doc.ApplyOperations([]Operation{{"s", []any{"n"}, 1}})
	}); err != nil {
		t.Fatal(err)
	}
	next, _, err := s.SnapshotDefinition(bg, def, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || any(next) != frames[0].Value || frames[0].Value == first {
		t.Fatal("snapshot and observer adopted different revisions")
	}
	if immutableViewMember(t, frames[0].Value, "keep") != immutableViewMember(t, first, "keep") || immutableViewMember(t, first, "n").Number() != json.Number("0") {
		t.Fatal("adoption copied an untouched branch or mutated the old revision")
	}
}

// Content-oriented legacy assertions opt into mutable copies. The readonly
// identity regressions above deliberately call the public methods directly.
type definitionSnapshotReader interface {
	SnapshotDefinition(context.Context, *DocumentDefinition, ID, *string) (ReadonlyValue, bool, error)
}
type definitionHistoricalReader interface {
	SnapshotDefinitionAsOf(context.Context, *DocumentDefinition, ID, *string, ID) (ReadonlyValue, bool, error)
}

func detachedDefinitionSnapshot(reader definitionSnapshotReader, ctx context.Context, def *DocumentDefinition, owner ID, key *string) (JSON, bool, error) {
	value, found, err := reader.SnapshotDefinition(ctx, def, owner, key)
	if err != nil || !found {
		return nil, found, err
	}
	return JSON(value.Copy().(map[string]any)), true, nil
}
func detachedDefinitionSnapshotAsOf(reader definitionHistoricalReader, ctx context.Context, def *DocumentDefinition, owner ID, key *string, at ID) (JSON, bool, error) {
	value, found, err := reader.SnapshotDefinitionAsOf(ctx, def, owner, key, at)
	if err != nil || !found {
		return nil, found, err
	}
	return JSON(value.Copy().(map[string]any)), true, nil
}
func detachedConversationDefinitionSnapshot(reader *ConversationHandle, ctx context.Context, def *DocumentDefinition, key *string) (JSON, bool, error) {
	value, found, err := reader.SnapshotDefinition(ctx, def, key)
	if err != nil || !found {
		return nil, found, err
	}
	return JSON(value.Copy().(map[string]any)), true, nil
}
func detachedConversationDefinitionSnapshotAsOf(reader *ConversationHandle, ctx context.Context, def *DocumentDefinition, key *string, at ID) (JSON, bool, error) {
	value, found, err := reader.SnapshotDefinitionAsOf(ctx, def, key, at)
	if err != nil || !found {
		return nil, found, err
	}
	return JSON(value.Copy().(map[string]any)), true, nil
}

func TestSessionV150ZeroRetentionKeepsHeldSnapshotIdentity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		window := math.Inf(1)
		f := openMainDeadlineSession(t, b, &window, 0)
		def, _ := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, f.session, def, 1, "held")
		first, found, err := f.session.SnapshotDefinition(bg, def, 1, nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		second, _, err := f.session.SnapshotDefinition(bg, def, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatal("zero idle retention discarded the held tracker identity")
		}
		if immutableViewMember(t, first, "text").String() != "held" {
			t.Fatal("held snapshot lost its memory value")
		}
	})
}

func TestSessionV150StoringHoldPreservesSnapshotIdentity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		window := math.Inf(1)
		f := openMainDeadlineSession(t, b, &window)
		def, _ := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, f.session, def, 1, "held")
		first, found, err := f.session.SnapshotDefinition(bg, def, 1, nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		if _, err := f.session.Commit(bg, func(tx *Tx) (any, error) {
			if _, err := tx.AcquireDocument(def, 1, nil, nil); err != nil {
				return nil, err
			}
			id, err := tx.mintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
		}); err != nil {
			t.Fatal(err)
		}
		second, _, err := f.session.SnapshotDefinition(bg, def, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatal("storing an already-adopted hold replaced its snapshot identity")
		}
	})
}

func TestSessionV150RequiredMigrationStoreKeepsSnapshotIdentity(t *testing.T) {
	s := gatedSession(t)
	options := DefinitionOptions{Kind: "oracle.readonly.migration", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }}
	old, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(old, 0, nil, nil); return nil, err }); err != nil {
		t.Fatal(err)
	}
	options.Version = 2
	calls := 0
	options.Migrate = func(value JSON, _ uint64) (JSON, error) { calls++; value["n"] = 10; return value, nil }
	next, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	before, _, err := s.SnapshotDefinition(bg, next, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(next, 0, nil, nil); return nil, err }); err != nil {
		t.Fatal(err)
	}
	after, _, err := s.SnapshotDefinition(bg, next, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if before != after || calls != 1 {
		t.Fatal("storing a required migration base rematerialized its readonly snapshot", calls)
	}
}

// Source-derived identity supplement to session-watches.test.ts:75. The source
// probe independently checks this exact state, two commits and delayed starts.
func TestSessionV150WatchSnapshotObserverSharedReadonlyRevision(t *testing.T) {
	runWatchSharedReadonlyRevision(t, false)
}
func TestSessionV150ZeroRetentionWatchSnapshotObserverSharedReadonlyRevision(t *testing.T) {
	runWatchSharedReadonlyRevision(t, true)
}
func runWatchSharedReadonlyRevision(t *testing.T, zero bool) {
	t.Helper()
	s := gatedSession(t)
	if zero {
		s.documentRetentionMs = func() int64 { return 0 }
	}
	def, err := DefineDocument(DefinitionOptions{Kind: "watch.state", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) {
		return JSON{"value": 0, "items": []any{"a", "b"}, "retained": JSON{"label": "stable"}}, nil
	}, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(def, 0, nil, nil); return nil, err }); err != nil {
		t.Fatal(err)
	}
	var first ReadonlyValue
	if !zero {
		var found bool
		first, found, err = s.SnapshotDefinition(bg, def, 0, nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
	}
	watches := []*DocumentWatch{}
	for range 2 {
		w, found, err := s.WatchDefinition(bg, def, 0, nil)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		defer w.Stop()
		watches = append(watches, w)
		v, err := w.Value()
		if first == nil {
			first = v
		}
		if err != nil || any(v) != first {
			t.Fatal("watch acquisition did not share snapshot identity", err)
		}
	}
	var observed ReadonlyValue
	observation, found, err := s.ObserveDefinition(bg, def, 0, nil, DocumentObserver{Advance: func(_ context.Context, call DocumentCall) error { observed = call.Value; return nil }, End: func(WatchEnd) error { return nil }})
	if err != nil || !found {
		t.Fatal(found, err)
	}
	defer observation.Detach()
	for _, n := range []int{1, 2} {
		if _, err = s.Commit(bg, func(tx *Tx) (any, error) {
			doc, err := tx.AcquireDocument(def, 0, nil, nil)
			if err != nil {
				return nil, err
			}
			return nil, doc.ApplyOperations([]Operation{{"s", []any{"value"}, n}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	latest, _, err := s.SnapshotDefinition(bg, def, 0, nil)
	if err != nil || latest != observed {
		t.Fatal("snapshot/observer revision", err)
	}
	calls := make(chan WatchFrame, 2)
	for _, w := range watches {
		v, err := w.Value()
		if err != nil || any(v) != first {
			t.Fatal("unstarted watch advanced", err)
		}
		if err = w.StartFrame(func(_ context.Context, frame WatchFrame) error {
			v, err := w.Value()
			if err != nil || any(v) != any(frame.Value) {
				t.Error("watch value not installed before callback", err)
			}
			calls <- frame
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		frame := watchReceive(t, calls)
		value, ok := any(frame.Value).(ReadonlyValue)
		if !ok || value != latest {
			t.Fatal("watch delivery did not share adopted revision")
		}
		if immutableViewMember(t, value, "retained") != immutableViewMember(t, first, "retained") {
			t.Fatal("watch copied unchanged child")
		}
		if len(frame.Ops) != 1 || frame.Ops[0][0] != "s" || frame.Ops[0][2].(json.Number) != "2" {
			t.Fatal("pending operations were not folded", frame.Ops)
		}
	}
	if immutableViewMember(t, first, "value").Copy().(json.Number) != "0" {
		t.Fatal("retained acquisition was mutated")
	}
}
