package durable

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Actual f9a26365 harness-ownership.test.ts ended-bridge case (depths 2 and 5):
// "aborts work below N ended tasks with their live owner, which waits for it". Built
// through the public transaction surface: `Tx` exposes CreateTask, CreateConversation,
// AppendEntry, CreateDocument, Document, MintID, CreateSubmission and AppendTypedEntry.
// The chain is p -> links (each owned by the previous), a conversation owned by the
// deepest link created while that owner is live, then `d` created in that conversation
// by a public commit after its owner ended. Aborting the live owner p aborts that work,
// and p's abort handler completes only after the member ended. Handler completion order
// is recorded as handlers settle. The direct task-ownership variant is not asserted here:
// native legitimately rejects new work owned by a decided task (`taskCanOwnNewWork`,
// task_definition.go:177), and the source does not test that shape.
func TestMainOwnershipEndedBridgeAbortsWorkBelowEndedTasks(t *testing.T) {
	for _, depth := range []int{2, 5} {
		t.Run("depth"+strconv.Itoa(depth), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				var mu sync.Mutex
				var order []string
				gates := map[string]chan struct{}{}
				links := make([]string, depth)
				for i := range links {
					links[i] = "link" + strconv.Itoa(depth) + "_" + strconv.Itoa(i)
				}
				names := append(append([]string{"p"}, links...), "d")
				for _, name := range names {
					gates[name] = make(chan struct{})
				}
				gates["abort.p"] = make(chan struct{})
				record := func(name string) {
					mu.Lock()
					order = append(order, name)
					mu.Unlock()
				}
				defs := map[string]*TaskDefinition{}
				for _, name := range names {
					def, err := DefineTask(TaskDefinitionOptions{Kind: "task.holdf." + strconv.Itoa(depth) + "." + name, Version: 1,
						Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
						Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
							select {
							case <-gates[name]:
							case <-ctx.Done():
								return ctx.Err()
							}
							return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
								record(name)
								return taskDone(name), nil
							})
						}},
						Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
							if g := gates["abort."+name]; g != nil {
								select {
								case <-g:
								case <-ctx.Done():
									return ctx.Err()
								}
							}
							return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
								record("abort." + name)
								return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
							})
						}})
					if err != nil {
						t.Fatal(err)
					}
					defs[name] = def
				}
				registry := NewRegistry()
				for _, def := range defs {
					if _, err := registry.RegisterTask(def); err != nil {
						t.Fatal(err)
					}
				}
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				c := root(t, h, ref)
				ids := map[string]ID{}
				var convID ID
				_, err := h.commitTasks(bg, c.ID(), func(tx *Tx) error {
					id, err := tx.CreateTask(defs["p"], JSON{"name": "p"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err != nil {
						return err
					}
					ids["p"] = id
					owner := id
					for _, name := range links {
						id, err = tx.CreateTask(defs[name], JSON{"name": name}, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
						if err != nil {
							return err
						}
						ids[name] = id
						owner = id
					}
					// Conversation owned by the deepest link, created while that owner is live.
					convID, err = tx.mintID()
					if err != nil {
						return err
					}
					if err := tx.createConversationRecord(Conversation{ID: convID, Owner: owner}); err != nil {
						return err
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				for i := depth - 1; i >= 0; i-- {
					close(gates[links[i]])
				}
				for _, name := range links {
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					rec, err := h.WaitForTask(ctx, ids[name])
					cancel()
					if err != nil {
						t.Fatal(name, "did not settle", rec, err)
					}
					if rec.State.Outcome == nil || rec.State.Outcome.Status != "completed" {
						t.Fatal(name, "not completed", rec.State)
					}
				}
				// Work created in the task-owned conversation by a public commit, after its owner ended.
				_, err = h.commitTasks(bg, convID, func(tx *Tx) error {
					id, err := tx.CreateTask(defs["d"], JSON{"name": "d"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err != nil {
						return err
					}
					ids["d"] = id
					return nil
				})
				if err != nil {
					t.Fatal("commit below ended tasks", err)
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(bg, 4*time.Second)
				defer cancel()
				var running TaskRecord
				deadline := time.Now().Add(3 * time.Second)
				for {
					var ok bool
					running, ok, err = h.Task(ctx, ids["d"])
					if err != nil || !ok {
						t.Fatal("work below ended tasks missing", ok, err)
					}
					if running.State.Status == "running" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("work below ended tasks never started", running.State.Status, running.State.Outcome)
					}
					time.Sleep(time.Millisecond)
				}
				if _, err := h.AbortTask(ctx, ids["p"]); err != nil {
					t.Fatal("abort", err)
				}
				ended, err := h.WaitForTask(ctx, ids["d"])
				if err != nil {
					t.Fatal("work below ended tasks did not settle", err)
				}
				if ended.State.Outcome == nil || ended.State.Outcome.Status != "aborted" {
					t.Error("work below ended tasks not aborted", ended.State)
				}
				mu.Lock()
				before := append([]string{}, order...)
				mu.Unlock()
				if !hasName(before, "abort.d") || hasName(before, "abort.p") {
					t.Error("p's handler settled before the work below ended tasks", before)
				}
				close(gates["abort.p"])
				pTask, err := h.WaitForTask(ctx, ids["p"])
				if err != nil {
					t.Fatal("p did not settle", err)
				}
				if pTask.State.Outcome == nil || pTask.State.Outcome.Status != "aborted" {
					t.Error("p not aborted", pTask.State)
				}
				mu.Lock()
				final := append([]string{}, order...)
				mu.Unlock()
				if len(final) != len(before)+1 || final[len(final)-1] != "abort.p" {
					t.Error("abort handler order", final)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func hasName(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
