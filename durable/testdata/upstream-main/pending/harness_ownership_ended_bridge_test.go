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
// "aborts work below N ended tasks with their live owner, which waits for it". The
// source creates the chain p -> links -> a conversation owned by the deepest ended
// link, then a commit creates `d` in that conversation; aborting p aborts `d` first
// and p's abort handler completes only after that member ended.
//
// Known-failing oracle. Two parts cannot be expressed through the native public API:
// `Tx` exposes no CreateConversation, so a task-owned conversation cannot be created,
// and the portable reduction (a public commit creating work owned by the deepest
// ended link) is rejected by native's own rule that a decided task cannot own new work
// (`task_definition.go:144`, `taskCanOwnNewWork` at `task_definition.go:177`), which
// contradicts the source's live-owner-holds rule. Baseline at 8a70e08, run through the
// project overlay with the protected prototype replaced only in the compiler input:
// 2 failing subtests (depth2 0.04s, depth5 0.06s), function 0.105s, both at
// "durable: rejected: task owner is decided or abort-marked"; the abort-order assertions
// are never reached. No assertion is weakened and no runtime change was made pending
// reconciliation of the live-owner/cross-owner export contracts.
func TestMainOwnershipEndedBridgeAbortsWorkBelowEndedTasks(t *testing.T) {
	for _, depth := range []int{2, 5} {
		t.Run("depth"+strconv.Itoa(depth), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				var mu sync.Mutex
				var order []string
				gates := map[string]chan struct{}{}
				links := make([]string, depth)
				for i := range links {
					links[i] = "link" + strconv.Itoa(i)
				}
				names := append(append([]string{"p"}, links...), "d")
				for _, name := range names {
					gates[name] = make(chan struct{})
				}
				gates["abort.p"] = make(chan struct{}) // p's handler stays blocked so order is observable
				record := func(name string) {
					mu.Lock()
					order = append(order, name)
					mu.Unlock()
				}
				defs := map[string]*TaskDefinition{}
				for _, name := range names {
					def, err := DefineTask(TaskDefinitionOptions{Kind: "task.hold." + strconv.Itoa(depth) + "." + name, Version: 1,
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
				_, err := h.CommitTasks(bg, c.ID(), func(tx *Tx) error {
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
				deepest := links[depth-1]
				_, err = h.CommitTasks(bg, c.ID(), func(tx *Tx) error {
					id, err := tx.CreateTask(defs["d"], JSON{"name": "d"}, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: ids[deepest]}})
					if err != nil {
						return err
					}
					ids["d"] = id
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(bg, 4*time.Second)
				defer cancel()
				running, ok, err := h.Task(ctx, ids["d"])
				if err != nil || !ok {
					t.Fatal("work below ended tasks missing", ok, err)
				}
				if running.State.Status != "running" {
					t.Fatal("work below ended tasks is not running", running.State)
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
				if !containsString(before, "abort.d") || containsString(before, "abort.p") {
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

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
