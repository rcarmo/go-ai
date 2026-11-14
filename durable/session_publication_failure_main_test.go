package durable

import (
	"context"
	"errors"
	"testing"
)

func TestMainSessionFailureDuringSettlementPreventsPublication(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		id, err := s.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		failure := errors.New("failure while storage is settling")
		appendFrame := s.core.appendFrame
		fired := false
		s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
			if appendFrame != nil {
				if err := appendFrame(kind, ordinal, high, payload); err != nil {
					return err
				}
			}
			if kind == 2 && !fired {
				fired = true
				_ = s.backendResult(bg, failure, true)
			}
			return nil
		}
		_, err = s.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "stored-not-published", Value: JSON{}})
		})
		if !fired || !errors.Is(err, failure) {
			t.Fatalf("settlement-time failure did not fence publication: fired=%v err=%v", fired, err)
		}
		if !errors.Is(s.failure(), failure) {
			t.Fatal("first failure lost", s.failure())
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := utf16ReopenStore(t, b.store)
		defer reopened.Close(bg)
		state, err := reopened.Snapshot(bg)
		if err != nil || state.Seq != 2 || len(state.Entries) != 1 {
			t.Fatal("stored receipt lost after failed publication", state, err)
		}
	})
}

func mainPrepareOnlyPublication(t *testing.T, s *Session, ctx context.Context, id ID, ready <-chan struct{}) preparedSessionPublication {
	t.Helper()
	state, err := s.taskSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx := &Tx{store: s.core, ctx: ctx, limits: s.limits, state: state, session: s}
	defer tx.seal()
	if err := tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "publication-stage", Value: JSON{}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.seal(); err != nil {
		t.Fatal(err)
	}
	if err := s.finalizeTaskWrites(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.finalizeDocuments(); err != nil {
		t.Fatal(err)
	}
	prepared, err := s.prepareSessionPublication(ctx, tx, state, ready)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

// mainSettleSerially settles a prepared publication directly with Storage. These
// tests isolate one stage of the publication pipeline, so they need the pre-writer
// serial path: admitting through the writer would hand the settle and the publish to
// a background flight and collapse the stages they are testing apart. The baseline
// guard is kept here because that is exactly the stage under test.
func mainSettleSerially(s *Session, ctx context.Context, prepared *preparedSessionPublication) (commitReceipt, Snapshot, error) {
	if prepared.base.Seq != s.readImage.Seq || s.core.state.Seq != s.readImage.Seq {
		return commitReceipt{}, Snapshot{}, reject("stale prepared publication baseline")
	}
	writes := prepared.storageWrites
	if writes == nil {
		writes = prepared.tx.writes
	}
	return s.settleBatch(ctx, Batch{Writes: writes}, prepared.base.Seq)
}

func mainPreparedPublication(t *testing.T, s *Session, ctx context.Context, id ID, ready <-chan struct{}) (preparedSessionPublication, commitReceipt, Snapshot) {
	t.Helper()
	prepared := mainPrepareOnlyPublication(t, s, ctx, id, ready)
	receipt, candidate, err := mainSettleSerially(s, ctx, &prepared)
	if err != nil {
		t.Fatal(err)
	}
	return prepared, receipt, candidate
}

func TestMainSessionPublicationStageFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"failure", "cancel-and-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				seedRootConversation(t, b.store)
				s, err := OpenSession(b.store)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				id, err := s.MintID(bg)
				if err != nil {
					t.Fatal(err)
				}
				_, sub, err := s.SubscribeCommits(bg)
				if err != nil {
					t.Fatal(err)
				}
				defer sub.Stop()
				ctx, cancel := context.WithCancel(bg)
				defer cancel()
				ready := make(chan struct{})
				defer close(ready)
				s.taskBookkeeping(func() {
					prepared, receipt, candidate := mainPreparedPublication(t, s, ctx, id, ready)
					if mode == "failure" {
						cause := errors.New("failure between settlement and publication")
						_ = s.backendResult(bg, cause, true)
						if _, err := s.publishSessionPublication(&prepared, receipt, &candidate); !errors.Is(err, cause) {
							t.Fatal("publication ignored failure", err)
						}
						if s.readImage.Seq != 1 {
							t.Fatal("failed Session adopted stored part")
						}
						return
					}
					cancel() // admitted durable work must still publish once
					if _, err := s.publishSessionPublication(&prepared, receipt, &candidate); err != nil {
						t.Fatal("cancellation abandoned stored part", err)
					}
					if s.readImage.Seq != 2 {
						t.Fatal("missing publication")
					}
					sub.mu.Lock()
					queued := len(sub.pending)
					sub.mu.Unlock()
					if queued != 1 {
						t.Fatal("missing prepared subscription frame", queued)
					}
					if _, err := s.publishSessionPublication(&prepared, receipt, &candidate); err == nil {
						t.Fatal("duplicate publication accepted")
					}
					sub.mu.Lock()
					after := len(sub.pending)
					sub.mu.Unlock()
					if after != queued || s.readImage.Seq != 2 {
						t.Fatal("duplicate publication changed observers or state")
					}
				})
			})
		})
	}
}
