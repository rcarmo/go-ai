package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Source f9a26365 harness-observers.test.ts:308: a task-owned document
// observation gets its retirement and then ends at the task's deciding commit,
// both outside storage adoption. This uses native private observer callbacks
// and explicit TaskRuntime/document IDs. The source's public observeDoc API and
// JS reference identity are not represented by this fixture.
func TestMainHarnessDocumentObserverOwnerRetirement(t *testing.T) {
	runHarnessDocumentObserverOwnerRetirement(t, false)
}

// Native derived control: an aborted deciding transaction must not retire the
// document or end its observer before the subsequent successful task outcome.
func TestMainHarnessDocumentObserverRollbackKeepsOwner(t *testing.T) {
	runHarnessDocumentObserverOwnerRetirement(t, true)
}

func runHarnessDocumentObserverOwnerRetirement(t *testing.T, testRollback bool) {
	t.Helper()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	def, err := DefineDocument(DefinitionOptions{
		Kind: "test.observers.progress", Version: 1, Scope: "task",
		Initial:        func(JSON) (JSON, error) { return JSON{"lines": []any{}}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	var gateOnce sync.Once
	release := func() { gateOnce.Do(func() { close(gate) }) }
	t.Cleanup(release)
	ready := make(chan *TaskRuntime, 1)
	taskDef := taskDefinition(t, "observe.p06.harness-owner", func(ctx context.Context, task TaskRecord, runtime *TaskRuntime) error {
		err := runtime.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			doc, err := tx.AcquireDocument(def, task.ID, nil, nil)
			if err != nil {
				return nil, err
			}
			return nil, doc.Update(func(value JSON) error { value["lines"] = append(value["lines"].([]any), "started"); return nil })
		})
		if err != nil {
			return err
		}
		ready <- runtime
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		return runtime.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	h := taskTestHarness(t, store, taskDef)
	// The harness cleanup was registered after the first gate cleanup; register
	// another release now so failure during attachment cannot strand Close.
	t.Cleanup(release)
	original := store.appendFrame
	var adopting atomic.Bool
	store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if kind == 2 {
			adopting.Store(true)
			defer adopting.Store(false)
		}
		return original(kind, ordinal, high, payload)
	}
	t.Cleanup(func() {
		release()
		_ = h.Close(context.Background())
		store.appendFrame = original
	})
	id := createPublicTask(t, h, taskDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	var running *TaskRuntime
	select {
	case running = <-ready:
		if running.TaskID() != id {
			t.Fatalf("started task id=%d want %d", running.TaskID(), id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("task did not publish its starting document")
	}
	// Native storage invokes its receipt callback while core.line is held.
	// The append flag alone does not cover that receipt/adoption interval.
	insideCore := func() bool {
		select {
		case <-store.line:
			store.leave()
			return false
		default:
			return true
		}
	}
	type call struct {
		kind     string
		frame    WatchFrame
		end      WatchEnd
		adopting bool
		coreHeld bool
	}
	calls := make(chan call, 3)
	observation, baseline, ok, err := h.session.observeDocumentDefinition(context.Background(), def, id, nil, sessionDocumentObserver{
		advance: func(_ context.Context, frame WatchFrame) error {
			calls <- call{kind: "advance", frame: frame, adopting: adopting.Load(), coreHeld: insideCore()}
			return nil
		},
		end: func(end WatchEnd) error {
			calls <- call{kind: "end", end: end, adopting: adopting.Load(), coreHeld: insideCore()}
			return nil
		},
	})
	if err != nil || !ok || observation == nil {
		t.Errorf("source308 task-document observation unavailable: ok=%v err=%v", ok, err)
		release()
		return
	}
	defer observation.detach()
	if !reflect.DeepEqual(baseline["lines"], []any{"started"}) {
		t.Fatalf("observation baseline: %v", baseline)
	}
	if testRollback {
		rollback := errors.New("rolled back owner retirement")
		if err := running.Commit(context.Background(), func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			if err := tx.RetireDefinition(def, id, nil); err != nil {
				return nil, err
			}
			return taskDone(nil), rollback
		}); !errors.Is(err, rollback) {
			t.Fatalf("rollback control: %v", err)
		}
		if len(calls) != 0 || !h.session.documentPinned(observation.id) {
			t.Fatal("rolled-back retirement delivered or released its pin")
		}
	}
	release()
	deadline, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	// As in the source, await task completion before inspecting observer calls.
	if _, err := h.WaitForTask(deadline, id); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"advance", "end"} {
		select {
		case got := <-calls:
			if got.kind != expected || got.adopting || got.coreHeld {
				t.Errorf("source308 %s: kind=%s append=%v core-held=%v", expected, got.kind, got.adopting, got.coreHeld)
			}
			if expected == "advance" && (!got.frame.Retired || !reflect.DeepEqual(got.frame.Value["lines"], []any{"started"}) || len(got.frame.Ops) != 0) {
				t.Errorf("source308 retirement: %+v", got.frame)
			}
			if expected == "end" && (got.end.Reason != "ended" || got.end.Error != nil) {
				t.Errorf("source308 ending: %+v", got.end)
			}
		default:
			t.Fatalf("source308 missing %s after task completion", expected)
		}
	}
	if err := h.Close(deadline); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Errorf("source308 close added observer calls: %d", len(calls))
	}
}

// Source session-observers.test.ts:241/254: an attachment waits for an earlier
// commit, or cancels without attaching. This exercises the private guard with
// a real Harness scheduler; it does not claim the source public API is present.
func TestMainHarnessDocumentObserverQueuedAttachment(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "after-commit"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := taskTestHarness(t, store)
			def, err := DefineDocument(DefinitionOptions{
				Kind: "test.observers.queued", Version: 1, Scope: "session",
				Initial:        func(JSON) (JSON, error) { return JSON{"count": 0}, nil },
				CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := sourceDocumentSet(h.session, def, 0, 1, false); err != nil {
				t.Fatal(err)
			}
			entered, gate := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			t.Cleanup(release)
			commitDone := make(chan error, 1)
			go func() {
				_, err := h.session.Commit(context.Background(), func(tx *Tx) error {
					doc, err := tx.AcquireDocument(def, 0, nil, nil)
					if err != nil {
						return err
					}
					if err := doc.ApplyOperations([]Operation{{"s", []any{"count"}, 11}}); err != nil {
						return err
					}
					close(entered)
					<-gate
					return nil
				})
				commitDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				release()
				t.Fatal("commit did not enter")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			admission := &mainCommitAdmissionContext{Context: ctx, entered: make(chan struct{})}
			type result struct {
				guard *sessionDocumentObservation
				value JSON
				ok    bool
				err   error
			}
			results := make(chan result, 1)
			calls := make(chan struct{}, 1)
			go func() {
				guard, value, ok, err := h.session.observeDocumentDefinition(admission, def, 0, nil, sessionDocumentObserver{
					advance: func(context.Context, WatchFrame) error { calls <- struct{}{}; return nil },
					end:     func(WatchEnd) error { return nil },
				})
				results <- result{guard, value, ok, err}
			}()
			select {
			case <-admission.entered:
			case <-time.After(3 * time.Second):
				release()
				t.Fatal("attachment never attempted admission")
			}
			if cancelled {
				cancel()
			}
			release()
			if err := <-commitDone; err != nil {
				t.Fatal(err)
			}
			got := <-results
			if cancelled {
				if got.guard != nil || got.value != nil || got.ok || !errors.Is(got.err, context.Canceled) {
					t.Fatalf("cancelled attachment: %+v", got)
				}
			} else {
				if got.err != nil || !got.ok || got.guard == nil || got.value["count"] != json.Number("11") {
					t.Fatalf("queued baseline: %+v", got)
				}
				got.guard.detach()
			}
			if len(calls) != 0 {
				t.Fatal("earlier commit delivered to later attachment")
			}
			h.session.observerMu.Lock()
			remaining := len(h.session.documentObservers)
			h.session.observerMu.Unlock()
			if remaining != 0 {
				t.Fatalf("attachment retained %d observers", remaining)
			}
		})
	}
}

// Source session-observers.test.ts:203: a backend rejection ends a document
// observation with the same first cause. Exercise a scheduler-backed Session
// through the real native append boundary, not the standalone-only path.
func TestMainHarnessDocumentObserverFailureCause(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, store)
	def, err := DefineDocument(DefinitionOptions{
		Kind: "test.observers.failure", Version: 1, Scope: "session",
		Initial:        func(JSON) (JSON, error) { return JSON{"count": 0}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceDocumentSet(h.session, def, 0, 1, false); err != nil {
		t.Fatal(err)
	}
	endings := make(chan WatchEnd, 2)
	advances := make(chan WatchFrame, 1)
	observation, _, ok, err := h.session.observeDocumentDefinition(context.Background(), def, 0, nil, sessionDocumentObserver{
		advance: func(_ context.Context, frame WatchFrame) error { advances <- frame; return nil },
		end:     func(end WatchEnd) error { endings <- end; return nil },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer observation.detach()
	original := store.appendFrame
	failure := &StorageRejected{Reason: "disk gone"}
	var inject atomic.Bool
	store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if kind == 2 && inject.Load() {
			return failure
		}
		return original(kind, ordinal, high, payload)
	}
	t.Cleanup(func() {
		_ = h.Close(context.Background())
		store.appendFrame = original
	})
	inject.Store(true)
	if err := sourceDocumentSet(h.session, def, 0, 2, false); !errors.Is(err, failure) {
		t.Fatalf("backend failure cause: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	closed, err := h.session.Closed(ctx)
	if err != nil || closed.Reason != "failed" || !errors.Is(closed.Error, failure) {
		t.Fatalf("failed Session: %+v err=%v", closed, err)
	}
	select {
	case end := <-endings:
		if end.Reason != "failed" || !errors.Is(end.Error, failure) {
			t.Fatalf("observer failure ending: %+v", end)
		}
	case <-ctx.Done():
		t.Fatal("observer failure ending not delivered")
	}
	if len(endings) != 0 || len(advances) != 0 {
		t.Fatalf("failure delivered extra events: endings=%d advances=%d", len(endings), len(advances))
	}
}
