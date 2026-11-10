package durable

import (
	"context"
	"encoding/json"
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
	if _, err = s.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 0, nil, nil); return err }); err != nil {
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
		_, err := s.Commit(bg, func(tx *Tx) error {
			doc, err := tx.AcquireDocument(def, 0, nil, nil)
			if err != nil {
				return err
			}
			return doc.Set(JSON{"n": 1})
		})
		stored <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(bg)
	cancel()
	beforeSlots := len(s.outcomeSlots)
	returned := make(chan error, 1)
	go func() {
		value, found, err := s.SnapshotDefinition(ctx, def, 0, nil)
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
	value, found, err := s.SnapshotDefinition(bg, def, 0, nil)
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
	if _, err = s.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(old, 0, nil, nil); return err }); err != nil {
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
		value, found, err := s.SnapshotDefinition(bg, def, 0, nil)
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
				_, e = s.Commit(bg, func(tx *Tx) error {
					id, e := tx.MintID()
					if e != nil {
						return e
					}
					_, e = tx.CreateDocument(Document{ID: id, Scope: "session", Kind: "retained", Version: 1, Value: JSON{"x": 1}})
					return e
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
					v, ok, e := s.SnapshotDefinition(bg, def, 0, nil)
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
