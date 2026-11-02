package durable

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// c9f4c7b9 session-observers.test.ts:389. Private standalone observer entry;
// source advance callbacks only record, never block/reenter/mutate publication.
func TestMainSyncCommitObserverCloseAfterLastPublication(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		s, add := mainSyncCounterSession(t, b)
		r := newMainSyncRecorder()
		if _, err := s.observeStoredCommits(r.observer()); err != nil {
			t.Fatal(err)
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		calls := r.callsCopy()
		if len(calls) != 3 || calls[0].end != nil || calls[1].end != nil || calls[0].seq == 0 || calls[1].seq == 0 || calls[2].end == nil {
			t.Fatal("stored advances then end", calls)
		}
		end := calls[2].end
		if end.Reason != "failed" || end.Error == nil || end.Error.Error() != "session closed" {
			t.Fatal("normal close observer end", end)
		}
	})
}

// Source403: controlled storage refusal, no advance, SAME ending cause. The
// native confirmed-refusal error is StorageRejected{Reason:"disk gone"}; a raw
// append I/O error intentionally becomes sanitized uncertainty, a separate case.
func TestMainSyncCommitObserverFailureCause(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		s, add := mainSyncCounterSession(t, b)
		r := newMainSyncRecorder()
		if _, err := s.observeStoredCommits(r.observer()); err != nil {
			t.Fatal(err)
		}
		failure := &StorageRejected{Reason: "disk gone"}
		var fail atomic.Bool
		fail.Store(true)
		s.taskBookkeeping(func() {
			original := s.core.appendFrame
			s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				if kind == 2 && fail.Swap(false) {
					return failure
				}
				if original != nil {
					return original(kind, ordinal, high, payload)
				}
				return nil
			}
		})
		if err := add(); !errors.Is(err, failure) || !strings.Contains(err.Error(), "disk gone") {
			t.Fatal("commit failure", err)
		}
		if _, err := s.Closed(bg); err != nil {
			t.Fatal(err)
		}
		calls := r.callsCopy()
		if len(calls) != 1 || calls[0].end == nil || calls[0].end.Reason != "failed" || calls[0].end.Error != failure {
			t.Fatal("raw first failure ending", calls)
		}
	})
}

// Source414 includes flush before detach. The native channel receives the real
// first callback; it adds no production acknowledgement or scheduling yield.
func TestMainSyncCommitObserverDetachNoEnd(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		s, add := mainSyncCounterSession(t, b)
		r := newMainSyncRecorder()
		observation, err := s.observeStoredCommits(r.observer())
		if err != nil {
			t.Fatal(err)
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		watchReceive(t, r.advanced)
		observation.detach()
		observation.detach()
		if err := add(); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		if calls := r.callsCopy(); len(calls) != 1 || calls[0].end != nil {
			t.Fatal("detached observer received more", calls)
		}
	})
}

// Source427. The private report hook adapts ReportingSession.report: it only
// records the emitted error/info. Generic/Harness reporter reentry stays unwired.
func TestMainSyncCommitObserverGuardedListenerError(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		s, add := mainSyncCounterSession(t, b)
		bug := errors.New("commit observer bug")
		var mu sync.Mutex
		thrown := 0
		var ends []WatchEnd
		var reports []struct {
			err  error
			info ReportInfo
		}
		if _, err := s.observeStoredCommits(sessionCommitObserver{
			advance: func(context.Context, CommitPublication) error { mu.Lock(); thrown++; mu.Unlock(); panic(bug) },
			end:     func(end WatchEnd) error { mu.Lock(); defer mu.Unlock(); ends = append(ends, end); return nil },
			report: func(_ context.Context, err error, info ReportInfo) {
				mu.Lock()
				defer mu.Unlock()
				reports = append(reports, struct {
					err  error
					info ReportInfo
				}{err, info})
			},
		}); err != nil {
			t.Fatal(err)
		}
		healthy := newMainSyncRecorder()
		if _, err := s.observeStoredCommits(healthy.observer()); err != nil {
			t.Fatal(err)
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		if err := add(); err != nil {
			t.Fatal(err)
		}
		watchReceive(t, healthy.advanced)
		watchReceive(t, healthy.advanced)
		mu.Lock()
		if thrown != 1 || len(ends) != 1 || ends[0].Reason != "listener_error" || ends[0].Error != bug || len(reports) != 1 || reports[0].err != bug || reports[0].info != (ReportInfo{Type: "listener_error"}) {
			t.Error("guard/report/original error", thrown, ends, reports)
		}
		mu.Unlock()
		if calls := healthy.callsCopy(); len(calls) != 2 {
			t.Fatal("healthy observer", calls)
		}
		if err := s.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(ends) != 1 || s.failure() != nil {
			t.Fatal("guard became fatal or ended twice", ends, s.failure())
		}
	})
}

type mainSyncCall struct {
	seq uint64
	end *WatchEnd
}
type mainSyncRecorder struct {
	mu       sync.Mutex
	calls    []mainSyncCall
	advanced chan struct{}
}

func newMainSyncRecorder() *mainSyncRecorder {
	return &mainSyncRecorder{advanced: make(chan struct{}, 16)}
}
func (r *mainSyncRecorder) observer() sessionCommitObserver {
	return sessionCommitObserver{
		advance: func(_ context.Context, p CommitPublication) error {
			r.mu.Lock()
			r.calls = append(r.calls, mainSyncCall{seq: p.Seq})
			r.mu.Unlock()
			r.advanced <- struct{}{}
			return nil
		},
		end: func(end WatchEnd) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.calls = append(r.calls, mainSyncCall{end: &end})
			return nil
		},
	}
}
func (r *mainSyncRecorder) callsCopy() []mainSyncCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mainSyncCall(nil), r.calls...)
}
func mainSyncCounterSession(t *testing.T, b backend) (*Session, func() error) {
	t.Helper()
	s, err := OpenSession(b.store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(bg); err != nil {
			t.Error(err)
		}
	})
	def, err := DefineDocument(DefinitionOptions{Kind: "observe.session-counter", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"count": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return s, func() error {
		_, err := s.Commit(bg, func(tx *Tx) error {
			h, err := tx.AcquireDocument(def, 0, nil, nil)
			if err != nil {
				return err
			}
			value, err := h.Get()
			if err != nil {
				return err
			}
			count, err := value["count"].(json.Number).Int64()
			if err != nil {
				return err
			}
			return h.ApplyOperations([]Operation{{"s", []any{"count"}, count + 1}})
		})
		return err
	}
}
