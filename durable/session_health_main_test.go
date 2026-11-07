package durable

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Only the private writer is concurrent here. Session commits remain serial.
// A published image must be readable before the blocked append settles, while
// later uncertain settlement must still fence reads and fail the Session.
func TestMainSessionPublishedReadDuringNativeAppend(t *testing.T) {
	for _, outcome := range []string{"success", "uncertain", "closing"} {
		t.Run(outcome, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				batches, ids := groupEntryBatches(t, b.store, 1)
				seedRootConversation(t, b.store)
				s, err := OpenSession(b.store)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				held, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				var workers sync.WaitGroup
				defer func() { unblock(); workers.Wait() }()
				appendFrame := s.core.appendFrame
				s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						close(held)
						<-release
						if outcome == "uncertain" {
							return errors.New("uncertain native append")
						}
					}
					if appendFrame != nil {
						return appendFrame(kind, ordinal, high, payload)
					}
					return nil
				}
				finished := make(chan error, 1)
				var receipt commitReceipt
				workers.Add(1)
				go func() {
					defer workers.Done()
					_, err := s.core.commitParts(bg, batches, func(r commitReceipt) error { receipt = r; return nil }, true)
					finished <- err
				}()
				select {
				case <-held:
				case <-time.After(5 * time.Second):
					t.Fatal("append never reached gate")
				}
				ctx, cancel := context.WithTimeout(bg, 2*time.Second)
				state, err := s.Snapshot(ctx)
				cancel()
				if err != nil || state.Seq != 1 || len(state.Entries) != 0 || state.HighWater != uint64(ids[0]) {
					t.Fatalf("published read waited for writer or imported raw data: seq=%d high=%d err=%v", state.Seq, state.HighWater, err)
				}
				ctx, cancel = context.WithCancel(bg)
				cancel()
				if _, err := s.Snapshot(ctx); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if outcome == "closing" {
					ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
					err := s.core.close(ctx, true)
					cancel()
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal("close did not wait for admitted writer", err)
					}
					if _, err := s.Snapshot(bg); !errors.Is(err, ErrClosed) {
						t.Fatal("close seal did not fence read", err)
					}
				}
				unblock()
				writeErr := <-finished
				workers.Wait()
				if outcome == "closing" {
					if writeErr != nil || receipt.seq != 2 {
						t.Fatal("close abandoned admitted settlement", writeErr, receipt.seq)
					}
					if err := s.Close(bg); err != nil {
						t.Fatal(err)
					}
					return
				}
				if outcome == "uncertain" {
					if !errors.Is(writeErr, ErrPoisoned) {
						t.Fatal(writeErr)
					}
					if _, err := s.Snapshot(bg); !errors.Is(err, ErrPoisoned) {
						t.Fatal("poisoned writer did not fence read", err)
					}
					return
				}
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				if state, err := s.Snapshot(bg); err != nil || state.Seq != 1 || len(state.Entries) != 0 {
					t.Fatal("unpublished append became visible", state, err)
				}
				s.taskBookkeeping(func() {
					if err := s.adoptReceipt(receipt, nil); err != nil {
						t.Fatal(err)
					}
				})
				id, err := s.MintID(bg)
				if err != nil || id != ids[0]+1 {
					t.Fatal(id, err)
				}
				if state, err := s.Snapshot(bg); err != nil || state.Seq != 2 || len(state.Entries) != 1 || state.HighWater != uint64(id) {
					t.Fatal("adoption/reservation lost", state, err)
				}
			})
		})
	}
}

type mainHealthSnapshotAdapter struct {
	nativeStorage
	reads     atomic.Int64
	firstRead chan struct{}
	readOnce  sync.Once
}

func (a *mainHealthSnapshotAdapter) Snapshot(ctx context.Context) (Snapshot, error) {
	a.reads.Add(1)
	a.readOnce.Do(func() { close(a.firstRead) })
	return a.nativeStorage.Snapshot(ctx)
}

func TestMainSessionWrappedReadRetainsWriterAdmission(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 1)
		adapter := &mainHealthSnapshotAdapter{nativeStorage: b.store.(nativeStorage), firstRead: make(chan struct{})}
		s, err := OpenSession(adapter)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		held, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var workers sync.WaitGroup
		defer func() { unblock(); workers.Wait() }()
		appendFrame := s.core.appendFrame
		s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
			close(held)
			<-release
			if appendFrame != nil {
				return appendFrame(kind, ordinal, high, payload)
			}
			return nil
		}
		finished := make(chan error, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := s.core.commitParts(bg, batches, nil, true)
			finished <- err
		}()
		select {
		case <-held:
		case <-time.After(5 * time.Second):
			t.Fatal("append never reached gate")
		}
		ctx, cancel := context.WithCancel(bg)
		defer cancel()
		read := make(chan error, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := s.Snapshot(ctx)
			read <- err
		}()
		select {
		case <-adapter.firstRead:
		case <-time.After(5 * time.Second):
			t.Fatal("adapter was not consulted")
		}
		cancel()
		if err := <-read; !errors.Is(err, context.Canceled) || adapter.reads.Load() != 1 {
			t.Fatal("adapter admission bypassed", err, adapter.reads.Load())
		}
		unblock()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		workers.Wait()
		state, err := s.Snapshot(bg)
		if err != nil || state.Seq != 2 || len(state.Entries) != 1 || adapter.reads.Load() != 2 {
			t.Fatal("adapter data lost", state, err, adapter.reads.Load())
		}
	})
}
