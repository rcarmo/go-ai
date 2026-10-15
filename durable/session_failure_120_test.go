package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type failingSnapshot120 struct {
	*MemoryStorage
	fail error
}

func (s *failingSnapshot120) Snapshot(ctx context.Context) (Snapshot, error) {
	if s.fail != nil {
		return Snapshot{}, s.fail
	}
	return s.MemoryStorage.Snapshot(ctx)
}
func TestSession120FatalReadLatchedAndClosed(t *testing.T) {
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	wrapper := &failingSnapshot120{MemoryStorage: store}
	s, e := OpenSession(wrapper)
	if e != nil {
		t.Fatal(e)
	}
	def := watchDoc(t, s)
	watch, _, e := s.WatchDefinition(bg, def, 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	cause := errors.New("backend unavailable")
	wrapper.fail = cause
	_, e = s.Snapshot(bg)
	var failure *SessionFailed
	if !errors.As(e, &failure) || !errors.Is(e, cause) {
		t.Fatal(e)
	}
	watchEnd, ended := watch.End()
	if !ended || watchEnd.Reason != "session_failed" || !errors.Is(watchEnd.Error, cause) {
		t.Fatal(watchEnd, ended)
	}
	if _, e = s.MintID(bg); !errors.As(e, &failure) {
		t.Fatal("failed session admitted reservation", e)
	}
	deadline, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	end, e := s.Closed(deadline)
	if e != nil || end.Reason != "failed" || !errors.Is(end.Error, cause) {
		t.Fatal(end, e)
	}
}
func TestSession120BackendMutationFailureIsFatalEvenRejected(t *testing.T) {
	for _, operation := range []string{"commit", "mint", "caught-mint"} {
		t.Run(operation, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				s, e := OpenSession(b.store)
				if e != nil {
					t.Fatal(e)
				}
				_, subscription, e := s.SubscribeCommits(bg)
				if e != nil {
					t.Fatal(e)
				}
				if operation == "commit" {
					if _, e = s.MintID(bg); e != nil {
						t.Fatal(e)
					}
				}
				before, e := s.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				cause := &StorageRejected{Reason: "injected backend mutation rejection"}
				original := s.core.appendFrame
				s.core.appendFrame = func(byte, uint64, uint64, []byte) error { return cause }
				if operation == "mint" {
					_, e = s.MintID(bg)
				} else if operation == "caught-mint" {
					_, e = s.Commit(bg, func(tx *Tx) error { _, _ = tx.MintID(); return nil })
				} else {
					_, e = s.Commit(bg, func(tx *Tx) error {
						return tx.AppendEntry(Entry{ID: 2, Conversation: 1, Kind: "test", Value: JSON{"x": 1}})
					})
				}
				var failed *SessionFailed
				if !errors.As(e, &failed) || !errors.Is(e, cause) {
					t.Fatal("backend rejection was not fatal", e)
				}
				end, ended := subscription.End()
				if !ended || end.Reason != "session_failed" || !errors.Is(end.Error, cause) {
					t.Fatal("failure notification", end, ended)
				}
				deadline, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				result, e := s.Closed(deadline)
				if e != nil || result.Reason != "failed" {
					t.Fatal(result, e)
				}
				s.core.appendFrame = original
				reopened := reopenStoreAfterHarnessClose(t, b.store)
				defer reopened.Close(bg)
				after, e := reopened.Snapshot(bg)
				if e != nil || after.Seq != before.Seq || after.HighWater != before.HighWater || len(after.Entries) != len(before.Entries) {
					t.Fatal("rejected mutation adopted", after, e)
				}
			})
		})
	}
}
func TestSession120HarnessFailureJoinsActualHostBeforeStorageClose(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		def := taskDefinition(t, "fatal-stubborn", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			return ctx.Err()
		})
		h := taskTestHarness(t, b.store, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, started)
		cause := errors.New("fatal snapshot")
		h.session.taskBookkeeping(func() { h.session.store = &failingSnapshotGeneric120{Storage: b.store, fail: cause} })
		if _, e := h.session.Snapshot(bg); !errors.Is(e, cause) {
			t.Fatal(e)
		}
		awaitTaskSignal(t, cancelled)
		ctx, cancel := context.WithCancel(bg)
		cancel()
		if _, e := h.session.Closed(ctx); !errors.Is(e, context.Canceled) {
			t.Fatal("close skipped host join", e)
		}
		if _, e := b.store.Snapshot(bg); e != nil {
			t.Fatal("closed storage before host returned", e)
		}
		releaseTaskGate(release)
		deadline, stop := context.WithTimeout(bg, 3*time.Second)
		defer stop()
		result, e := h.session.Closed(deadline)
		if e != nil || result.Reason != "failed" {
			t.Fatal(result, e)
		}
		if e = h.Close(deadline); e != nil {
			t.Fatal(e)
		}
		reopened := reopenStoreAfterHarnessClose(t, b.store)
		defer reopened.Close(bg)
		state, e := reopened.Snapshot(bg)
		if e != nil || taskHasDecidedOutcome(state.Tasks[id]) {
			t.Fatal("invented outcome on failure", state.Tasks[id], e)
		}
	})
}

func TestSession120LocalValidationDoesNotFailBackend(t *testing.T) {
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	cause := reject("caller callback rejected")
	if _, e = s.Commit(bg, func(*Tx) error { return cause }); !errors.Is(e, cause) {
		t.Fatal(e)
	}
	if _, e = s.Commit(bg, func(tx *Tx) error { return tx.AppendEntry(Entry{ID: 99, Conversation: 1, Kind: "test", Value: JSON{}}) }); e == nil {
		t.Fatal("unreserved entry accepted")
	}
	if _, e = s.Snapshot(bg); e != nil {
		t.Fatal("local validation failed Session", e)
	}
	if e = s.Close(bg); e != nil {
		t.Fatal(e)
	}
	result, e := s.Closed(bg)
	if e != nil || result.Reason != "closed" || result.Error != nil {
		t.Fatal(result, e)
	}
}
func TestSession120CallerCancelledBackendReadDoesNotFailSession(t *testing.T) {
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	wrapper := &cancelRead120{MemoryStorage: store}
	s, e := OpenSession(wrapper)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	ctx, cancel := context.WithCancel(bg)
	wrapper.cancel = cancel
	if _, e = s.Snapshot(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	wrapper.cancel = nil
	if _, e = s.Snapshot(bg); e != nil {
		t.Fatal("cancelled backend read failed Session", e)
	}
}

type cancelRead120 struct {
	*MemoryStorage
	cancel context.CancelFunc
}

func (s *cancelRead120) Snapshot(ctx context.Context) (Snapshot, error) {
	if s.cancel != nil {
		s.cancel()
		return Snapshot{}, ctx.Err()
	}
	return s.MemoryStorage.Snapshot(ctx)
}

type failingSnapshotGeneric120 struct {
	Storage
	fail error
}

func (s *failingSnapshotGeneric120) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{}, s.fail
}

func TestSession120InvalidReadAndCallerCancellationAreRecoverable(t *testing.T) {
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	wrapper := &failingSnapshot120{MemoryStorage: store}
	s, e := OpenSession(wrapper)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	wrapper.fail = storageRequest("invalid query")
	_, e = s.Snapshot(bg)
	var request *StorageRequestError
	if !errors.As(e, &request) {
		t.Fatal(e)
	}
	wrapper.fail = nil
	if _, e = s.Snapshot(bg); e != nil {
		t.Fatal("request failed session", e)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, e = s.Snapshot(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e = s.MintID(bg); e != nil {
		t.Fatal("cancelled read failed session", e)
	}
}

func TestSession120HarnessFatalCauseReportedOnceOutsideLine(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		cause := errors.New("first fatal read")
		var reports atomic.Int64
		reported := make(chan error, 1)
		var h *Harness
		h = openHarness(t, b.store, Options{OnReport: func(err error) {
			reports.Add(1)
			_, _ = h.Snapshot(bg)
			// A reporting callback may synchronously wait for shutdown. Its
			// delivery must not be a prerequisite of that same shutdown.
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			if _, closeErr := h.session.Closed(ctx); closeErr != nil {
				reported <- closeErr
				return
			}
			reported <- err
			panic("reporter panic must be isolated")
		}})
		h.session.store = &failingSnapshotGeneric120{h.session.store, cause}
		if _, err := h.Snapshot(bg); !errors.Is(err, cause) {
			t.Fatal(err)
		}
		h.scheduler.report(&SessionFailed{Cause: cause})
		if _, err := h.Snapshot(bg); !errors.Is(err, cause) {
			t.Fatal(err)
		}
		select {
		case err := <-reported:
			if !errors.Is(err, cause) {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("fatal cause not reported/reentrant callback blocked")
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if end, err := h.session.Closed(ctx); err != nil || end.Reason != "failed" {
			t.Fatal(end, err)
		}
		if reports.Load() != 1 {
			t.Fatal("fatal cause reported repeatedly", reports.Load())
		}
		// Do not hide an unrelated error merely because a host wrapped it in
		// SessionFailed; only this Session's latched backend cause is deduped.
		other := errors.New("unrelated host failure")
		h.scheduler.report(&SessionFailed{Cause: other})
		if err := <-reported; !errors.Is(err, other) || reports.Load() != 2 {
			t.Fatal("unrelated error suppressed", err, reports.Load())
		}
	})
}
