package durable

import (
	"context"
	"sync"
	"testing"
	"time"
)

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
