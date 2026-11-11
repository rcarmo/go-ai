package durable

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// Pinned session.ts observeDocument/endWatch and harness-observers.test.ts:
// terminal owners detach immediately, but normal completion waits for delivery.
func TestMainWatchTaskOwnerEnd(t *testing.T) {
	for _, mode := range []string{"started", "unstarted", "busy-final", "busy-before", "already-retired", "close", "cancel", "stop", "listener-error", "failure", "failed-owner", "aborted-owner"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				hostRelease := make(chan struct{})
				runtimeReady := make(chan *TaskRuntime, 1)
				work := taskDefinition(t, "watch.owner", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					runtimeReady <- r
					select {
					case <-hostRelease:
					case <-ctx.Done():
					}
					return nil
				})
				h := taskTestHarness(t, b.store, work)
				defer close(hostRelease)
				id := createPublicTask(t, h, work, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if e := h.Resume(bg); e != nil {
					t.Fatal(e)
				}
				runtime := watchReceive(t, runtimeReady)
				var e error
				def := checkpointDef(t, DefinitionOptions{Kind: "watch.owner.progress", Scope: "task", Version: 1,
					Initial:        func(JSON) (JSON, error) { return JSON{"n": 0}, nil },
					CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
				})
				if e = runtime.Commit(bg, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					_, e := tx.AcquireDocument(def, id, nil, nil)
					return nil, e
				}); e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithCancel(bg)
				defer cancel()
				w, ok, e := h.session.WatchDefinition(ctx, def, id, nil)
				if e != nil || !ok {
					t.Fatal(ok, e)
				}
				defer w.Stop()
				w.mu.Lock()
				docID := w.id
				w.mu.Unlock()
				frames := make(chan rawWatchFrame, 3)
				callbackGate := make(chan struct{})
				defer func() {
					select {
					case <-callbackGate:
					default:
						close(callbackGate)
					}
				}()
				listenerError := errors.New("owner watch listener")
				listener := func(_ context.Context, f rawWatchFrame) error {
					frames <- f
					if mode == "busy-final" || (mode == "busy-before" && !f.Retired) {
						<-callbackGate
					}
					if mode == "listener-error" {
						return listenerError
					}
					return nil
				}
				if mode == "started" || mode == "busy-final" || mode == "busy-before" || mode == "already-retired" || mode == "listener-error" || mode == "failed-owner" || mode == "aborted-owner" {
					if e = startDetachedDocumentFrames(w, listener); e != nil {
						t.Fatal(e)
					}
				}
				if mode == "busy-before" {
					if e = runtime.Commit(bg, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						doc, err := tx.AcquireDocument(def, id, nil, nil)
						if err != nil {
							return nil, err
						}
						return nil, doc.ApplyOperations([]Operation{{"s", []any{"n"}, 1}})
					}); e != nil {
						t.Fatal(e)
					}
					if f := watchReceive(t, frames); f.Retired {
						t.Fatal("wrong in-flight call", f)
					}
				}
				if mode == "already-retired" {
					if e = runtime.Commit(bg, func(tx *Tx, _ TaskRecord) (*TaskState, error) { return nil, tx.RetireDefinition(def, id, nil) }); e != nil {
						t.Fatal(e)
					}
					f := watchReceive(t, frames)
					if !f.Retired {
						t.Fatal(f)
					}
				}
				rollback := errors.New("rollback owner ending")
				if e = runtime.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("not stored"), rollback }); !errors.Is(e, rollback) {
					t.Fatal(e)
				}
				w.mu.Lock()
				ending := w.ending
				w.mu.Unlock()
				if ending {
					t.Fatal("rollback detached watch")
				}
				if e = runtime.Commit(bg, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					if mode != "already-retired" {
						doc, e := tx.AcquireDocument(def, id, nil, nil)
						if e != nil {
							return nil, e
						}
						if e = doc.ApplyOperations([]Operation{{"s", []any{"n"}, 99}}); e != nil {
							return nil, e
						}
					}
					if mode == "failed-owner" {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "owner failed"}}}, nil
					}
					if mode == "aborted-owner" {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					}
					return taskDone("finished"), nil
				}); e != nil {
					t.Fatal(e)
				}
				if h.session.documentPinned(docID) {
					t.Fatal("terminal owner retained incarnation pin")
				}
				if _, ok, e := h.session.WatchDefinition(bg, def, id, nil); e != nil || ok {
					t.Fatal("watched terminal task document", ok, e)
				}
				reason := "ended"
				switch mode {
				case "close":
					if _, ended := w.End(); ended {
						t.Fatal("unstarted final delivery ended early")
					}
					if e = h.Close(bg); e != nil {
						t.Fatal(e)
					}
					reason = "session_closed"
				case "failure":
					cause := errors.New("injected backend first cause")
					failure := h.session.backendResult(bg, cause, true)
					watchReceive(t, w.Closed())
					if end, _ := w.End(); !errors.Is(end.Error, cause) || !errors.Is(failure, cause) {
						t.Fatal("lost first failure", end)
					}
					reason = "session_failed"
				case "cancel":
					cancel()
					reason = "cancelled"
				case "stop":
					w.Stop()
					reason = "stopped"
				case "already-retired":
				default:
					if mode == "unstarted" {
						if _, ended := w.End(); ended {
							t.Fatal("unstarted final delivery ended early")
						}
						if e = startDetachedDocumentFrames(w, listener); e != nil {
							t.Fatal(e)
						}
					}
					wantValue := json.Number("0")
					if mode == "busy-before" {
						if _, ended := w.End(); ended {
							t.Fatal("ended ahead of queued retirement")
						}
						close(callbackGate)
						wantValue = "1"
					}
					f := watchReceive(t, frames)
					if !f.Retired || len(f.Ops) != 0 || f.Value["n"].(json.Number) != wantValue {
						t.Fatal("wrong final retirement", f)
					}
					if mode == "busy-final" {
						if _, ended := w.End(); ended {
							t.Fatal("ended before final callback returned")
						}
						close(callbackGate)
					}
					if mode == "listener-error" {
						reason = "listener_error"
					}
				}
				watchReceive(t, w.Closed())
				end, ended := w.End()
				if !ended || end.Reason != reason {
					t.Fatal(end, ended, reason)
				}
				if mode == "listener-error" && !errors.Is(end.Error, listenerError) {
					t.Fatal(end)
				}
				h.session.observerMu.Lock()
				registered := h.session.watches[w]
				h.session.observerMu.Unlock()
				if registered {
					t.Fatal("ended watch retained Session registration")
				}
			})
		})
	}
}
