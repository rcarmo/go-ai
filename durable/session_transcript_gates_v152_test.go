package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// Hold the actual public Storage call before any write or receipt.
type transcriptGateStorage struct {
	Storage
	hold    atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (s *transcriptGateStorage) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	if s.hold.Swap(false) {
		close(s.entered)
		<-s.release
	}
	return s.Storage.Commit(ctx, batches, receive)
}

// The witness counts Session waits only; Storage's own cancellation checks
// must not be mistaken for a wait on an unpublished dependency.
func (s *transcriptGateStorage) ScanEntries(ctx context.Context, q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	if gate, ok := ctx.(*documentGateContext); ok {
		ctx = gate.Context
	}
	return s.Storage.ScanEntries(ctx, q, limit, cursor)
}

func (s *transcriptGateStorage) FindLatestHeadMarker(ctx context.Context, conversation, at ID) (Entry, bool, error) {
	if gate, ok := ctx.(*documentGateContext); ok {
		ctx = gate.Context
	}
	return s.Storage.FindLatestHeadMarker(ctx, conversation, at)
}

type transcriptRead func(*Session, context.Context, ID, func(ID, error) error) error

func transcriptReads() map[string]transcriptRead {
	scanID := func(page Page[Entry], err error) (ID, error) {
		if err != nil || len(page.Items) == 0 {
			return 0, err
		}
		return page.Items[0].ID, nil
	}
	optionalID := func(id ID, present bool, err error) (ID, error) {
		if err == nil && present != (id != 0) {
			err = fmt.Errorf("inconsistent presence for entry ID %v: %v", id, present)
		}
		return id, err
	}
	txRead := func(read func(*Tx, ID) (ID, error)) transcriptRead {
		return func(s *Session, ctx context.Context, id ID, check func(ID, error) error) error {
			_, err := s.Commit(ctx, func(tx *Tx) (any, error) { return nil, check(read(tx, id)) })
			return err
		}
	}
	lineRead := func(read func(*ReadLine, ID) (ID, error)) transcriptRead {
		return func(s *Session, ctx context.Context, id ID, check func(ID, error) error) error {
			return s.ReadOnLine(ctx, func(line *ReadLine) error { return check(read(line, id)) })
		}
	}
	return map[string]transcriptRead{
		"latestEntryId": txRead(func(tx *Tx, id ID) (ID, error) {
			return optionalID(tx.LatestEntryID(id))
		}),
		"latestHeadMarker": txRead(func(tx *Tx, id ID) (ID, error) {
			e, found, err := tx.LatestHeadMarker(id)
			return optionalID(e.ID, found, err)
		}),
		"tx.scanEntries": txRead(func(tx *Tx, id ID) (ID, error) {
			return scanID(tx.ScanEntries(EntryQuery{Conversation: id}, 1, nil))
		}),
		"line.scanEntries": lineRead(func(line *ReadLine, id ID) (ID, error) {
			return scanID(line.ScanEntries(EntryQuery{Conversation: id}, 1, nil))
		}),
		"line.findLatestHeadMarker": lineRead(func(line *ReadLine, id ID) (ID, error) {
			e, found, err := line.FindLatestHeadMarker(id, 0)
			return optionalID(e.ID, found, err)
		}),
	}
}

// Translation of session-group-commit.test.ts:255: all five reads return
// absence (or an empty page) after the conversation becomes visible. Observe
// the wait itself as well as the result, and exercise the append key and
// unrelated progress separately as supplemental implementation probes.
func TestSourceV152TranscriptReadGates(t *testing.T) {
	for name, read := range transcriptReads() {
		for _, mode := range []string{"pending-creation", "pending-append", "unrelated-append"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				store, err := openSQLiteTestDirectory(t.TempDir(), SQLiteOptions{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close(bg) })
				if mode != "pending-creation" {
					seedRootConversation(t, store)
				}
				storage := &transcriptGateStorage{Storage: store, entered: make(chan struct{}), release: make(chan struct{})}
				s, err := OpenSession(storage)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.Close(bg) })
				makeConversation := func(tx *Tx) (Conversation, error) {
					return tx.CreateConversation(JSON{"ownership": JSON{"kind": "ownerless"}})
				}
				var conversation ID = 1
				if mode == "unrelated-append" {
					v, err := s.Commit(bg, func(tx *Tx) (any, error) { return makeConversation(tx) })
					if err != nil {
						t.Fatal(err)
					}
					conversation = v.(Conversation).ID
				}
				storage.hold.Store(true)
				release := func() {
					select {
					case <-storage.release:
					default:
						close(storage.release)
					}
				}
				defer release()
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				leaked := make(chan ID, 1)
				first := make(chan error, 1)
				go func() {
					_, err := s.Commit(ctx, func(tx *Tx) (any, error) {
						if mode == "pending-creation" {
							c, err := makeConversation(tx)
							leaked <- c.ID
							return nil, err
						}
						e, err := tx.AppendEntry(1, JSON{"kind": "summary", "head": "self"})
						leaked <- e.ID
						return nil, err
					})
					first <- err
				}()
				select {
				case <-storage.entered:
				case err := <-first:
					t.Fatalf("write ended before it reached Storage: %v", err)
				case <-ctx.Done():
					t.Fatal("write did not reach Storage")
				}
				var want ID
				switch id := <-leaked; mode {
				case "pending-creation":
					conversation = id
				case "pending-append":
					want = id
				}
				gate := &documentGateContext{Context: ctx, queued: make(chan struct{})}
				reached, done := make(chan error, 1), make(chan error, 1)
				go func() {
					done <- read(s, gate, conversation, func(id ID, err error) error {
						if err == nil && id != want {
							err = fmt.Errorf("read got ID %v, want %v", id, want)
						}
						reached <- err
						return err
					})
				}()
				if mode == "unrelated-append" {
					select {
					case err := <-reached:
						if err != nil {
							t.Fatal(err)
						}
					case <-gate.queued:
						t.Fatal("unrelated read waited on the held append")
					case <-ctx.Done():
						t.Fatal("unrelated read did not run")
					}
				} else {
					select {
					case <-gate.queued:
					case err := <-reached:
						t.Fatalf("dependent read ran before publication: %v", err)
					case <-ctx.Done():
						t.Fatal("dependent read did not reach its publication gate")
					}
				}
				release()
				if err := settleWithinGate(t, first, 3); err != nil {
					t.Fatal(err)
				}
				if err := settleWithinGate(t, done, 3); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
