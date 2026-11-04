package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Known-failing oracle for f9a26365 packages/durable/test/harness-group-commit.test.ts case 758,
// "signals an invocation when it closes during a call in flight, and stores that call before Storage
// closes".
//
// Source assertion: the running invocation is signalled while a call is in flight, and close does not
// settle before that call is stored.
//
// Measured native baseline (revision c0ec30e, single run, count=1): close does wait for the in-flight
// call, but it does not signal the running invocation during that window — the invocation is only
// signalled after the held call settles. The storage-before-close and reopened-pending halves of the
// case pass unchanged (entry present after reopen, invocation pending again).
//
// Run with the project overlay so the protected continuation prototype stays out of compiler input:
//
//	make test PACKAGES=./durable TEST_FLAGS="-overlay <run>/overlay.json -v -run '^TestPendingGroupCommitCloseSignalsInvocationDuringCallInFlight' -count=1 -timeout=45s"
func TestPendingGroupCommitCloseSignalsInvocationDuringCallInFlight(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	held := &groupCommitHeld{entered: make(chan struct{}), release: make(chan struct{})}
	var signalled atomic.Bool
	def, err := DefineTask(TaskDefinitionOptions{
		Kind:    "task.gcclose.listens",
		Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "wait"}, nil },
		Phases: map[string]TaskPhase{"wait": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			<-ctx.Done()
			signalled.Store(true)
			return nil
		}},
		Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "close"}}, nil
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	original := h.session.core.appendFrame
	h.session.core.appendFrame = held.install(original)
	defer func() { h.session.core.appendFrame = original }()

	c := root(t, h, ref)
	ctx, cancel := context.WithTimeout(bg, 20*time.Second)
	defer cancel()
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	var id ID
	if _, err := h.CommitTasks(ctx, c.ID(), func(tx *Tx) error {
		var err error
		id, err = tx.CreateTask(def, JSON{"name": "one"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		rec, ok, err := h.Task(ctx, id)
		if err != nil || !ok {
			t.Fatal("task missing", ok, err)
		}
		if rec.State.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task never started", rec.State.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}

	held.holdCommits()
	writing := make(chan error, 1)
	go func() {
		_, err := h.session.Commit(ctx, func(tx *Tx) error {
			eid, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: eid, Conversation: c.ID(), Kind: "note", Value: JSON{"body": "note"}})
		})
		writing <- err
	}()
	select {
	case <-held.entered:
	case err := <-writing:
		t.Fatalf("in-flight call settled before reaching storage: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight call did not reach storage while held")
	}

	closing := make(chan error, 1)
	go func() { closing <- h.Close(ctx) }()
	select {
	case err := <-closing:
		t.Fatalf("close settled before the in-flight call was stored: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	// Source assertion, unmet natively.
	if !signalled.Load() {
		held.releaseHold()
		t.Fatal("close did not signal the running invocation during the call in flight")
	}

	// Retained assertions that pass unchanged.
	select {
	case err := <-closing:
		if err != nil {
			t.Fatalf("close after release: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close stuck after release")
	}
	select {
	case err := <-writing:
		if err != nil {
			t.Fatalf("in-flight call after release: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight call stuck after release")
	}
	reopened := reopenStoreAfterHarnessClose(t, store)
	defer reopened.Close(ctx)
	s, err := OpenSession(reopened)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Entries) != 1 {
		t.Fatalf("in-flight call was not stored before close: %d entries", len(snap.Entries))
	}
	if snap.Tasks[id].Execution == nil {
		t.Fatal("reopened task has no execution")
	}
	if status := snap.Tasks[id].Execution.Native.State.Status; status != "pending" {
		t.Fatalf("reopened invocation must be pending, got %s", status)
	}
}
