package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Pinned harness-group-commit.test.ts:758. Before the close registry/fence this
// test failed because a held storage call prevented invocation signalling.
// Source releases the held call after checking the signal, before awaiting
// Close. Admitted storage must land, and the interrupted task reopens pending.
func TestV150GroupCommitCloseSignalsInvocationDuringCallInFlight(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	held := &groupCommitHeld{entered: make(chan struct{}), release: make(chan struct{})}
	var signalled atomic.Bool
	entered := make(chan struct{})
	def, err := DefineTask(TaskDefinitionOptions{
		Kind:    "task.gcclose.listens",
		Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "wait"}, nil },
		Phases: map[string]TaskPhase{"wait": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-ctx.Done()
			// Source-derived authority fence: even a fresh caller context cannot
			// run another commit callback once Close has signalled this invocation.
			called := false
			err := r.Commit(context.Background(), func(*Tx, TaskRecord) (*TaskState, error) { called = true; return taskDone(nil), nil })
			if called || !errors.Is(err, ErrSealed) {
				t.Errorf("late invocation commit called=%v error=%v", called, err)
			}
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

	// A Go reservation is published before its handler goroutine runs.
	// Observe the actual listener before testing its cancellation, rather than
	// mistaking a sealed-before-start reservation for an unsignalled handler.
	awaitTaskSignal(t, entered)
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

	// Source releases the held backend call before awaiting close.
	held.releaseHold()

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
