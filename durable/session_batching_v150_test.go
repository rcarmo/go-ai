package durable

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Pinned test/session-group-commit.test.ts:170: disjoint callbacks are admitted
// during a held call, then the writer sends both waiting parts in one call.
func TestSessionV150DisjointDocumentAdmissionBatchesWaitingParts(t *testing.T) {
	for _, withHarness := range []bool{false, true} {
		name := "session"
		if withHarness {
			name = "harness"
		}
		t.Run(name, func(t *testing.T) {
			var s *Session
			var commitChange func(context.Context, func(*Tx) error) (uint64, error)
			if withHarness {
				store, err := newTestSQLite(t)
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, store)
				s = h.session
				commitChange = h.Commit
			} else {
				s = gatedSession(t)
				commitChange = s.Commit
			}

			def, err := DefineDocument(DefinitionOptions{Kind: "batch.note", Scope: "conversation", History: "latest", Fork: "initial", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err != nil {
				t.Fatal(err)
			}
			keys := []string{"a", "b", "c"}
			owners := []ID{1}
			if _, err = commitChange(bg, func(tx *Tx) error {
				for i := 0; i < 2; i++ {
					id, e := tx.MintID()
					if e != nil {
						return e
					}
					if e := tx.CreateConversation(Conversation{ID: id}); e != nil {
						return e
					}
					owners = append(owners, id)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			var mu sync.Mutex
			var calls []int
			commit := s.writer.host.commit
			s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
				mu.Lock()
				calls = append(calls, len(batches))
				first := len(calls) == 1
				mu.Unlock()
				if first {
					close(entered)
					<-release
				}
				return commit(ctx, batches, receive)
			}
			var seqs []uint64
			observation, err := s.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p CommitPublication) error {
				mu.Lock()
				seqs = append(seqs, p.Seq)
				for _, change := range p.Documents {
					if change.Record.CreatedAt != p.Seq {
						t.Errorf("createdAt=%d, receipt=%d", change.Record.CreatedAt, p.Seq)
					}
				}
				mu.Unlock()
				return nil
			}, end: func(WatchEnd) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer observation.detach()
			results := make([]chan error, 3)
			ran := make(chan string, 3)
			for i := range keys {
				results[i] = make(chan error, 1)
				i := i
				go func() {
					_, e := commitChange(bg, func(tx *Tx) error {
						doc, e := tx.AcquireDocument(def, owners[i], nil, nil)
						if e != nil {
							return e
						}
						if e = doc.Set(JSON{"n": 1}); e != nil {
							return e
						}
						ran <- keys[i]
						return nil
					})
					results[i] <- e
				}()
				if i == 0 {
					<-entered
					<-ran
					continue
				}
				select {
				case <-ran:
				case <-time.After(2 * time.Second):
					unblock()
					for j := 0; j <= i; j++ {
						<-results[j]
					}
					t.Fatal("disjoint callback blocked behind the held writer")
				}
			}
			// Queue a read after all three line parts, so its gate witnesses that both
			// later commits were admitted, not merely that their callbacks returned.
			queued := make(chan struct{})
			readDone := make(chan error, 1)
			go func() { _, e := s.Snapshot(&documentGateContext{Context: bg, queued: queued}); readDone <- e }()
			awaitTaskSignal(t, queued)
			mu.Lock()
			early := len(seqs)
			mu.Unlock()
			if early != 0 {
				t.Error("published before storage release")
			}
			unblock()
			for _, done := range results {
				if e := <-done; e != nil {
					t.Error(e)
				}
			}
			if e := <-readDone; e != nil {
				t.Error(e)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(calls, []int{1, 2}) {
				t.Errorf("calls %v, want [1 2]", calls)
			}
			if len(seqs) != 3 || seqs[1] != seqs[0]+1 || seqs[2] != seqs[1]+1 {
				t.Errorf("publication sequences %v", seqs)
			}
		})
	}
}
