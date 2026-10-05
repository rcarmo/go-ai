package durable

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func observeTaskState(t *testing.T, h *Harness, id ID, status string) TaskRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		record, ok, err := h.Task(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if ok && record.State.Status == status {
			return record
		}
		select {
		case <-ctx.Done():
			t.Fatal("state barrier", id, status)
		default:
			runtime.Gosched()
		}
	}
}
func TestTaskOwnershipHeldFailureFailFastBeforeDescendantDrain(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		grandAbort := make(chan struct{})
		releaseGrand := make(chan struct{})
		siblingAbort := make(chan struct{})
		outsideEntered, releaseOutside, outsideTerminal, returnOutside := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var outsideRuntime *TaskRuntime
		var outsideAborts atomic.Int64
		outside := taskDefinition(t, "task.failfast.outside", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			outsideRuntime = r
			close(outsideEntered)
			<-releaseOutside
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("outside"), nil }); err != nil {
				return err
			}
			close(outsideTerminal)
			<-returnOutside
			return nil
		})
		outside.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			outsideAborts.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		t.Cleanup(func() {
			select {
			case <-releaseGrand:
			default:
				close(releaseGrand)
			}
		})
		grand := taskDefinition(t, "task.failfast.grand", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error { <-ctx.Done(); return ctx.Err() })
		grand.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(grandAbort)
			<-releaseGrand
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		sibling := taskDefinition(t, "task.failfast.sibling", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error { <-ctx.Done(); return ctx.Err() })
		sibling.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(siblingAbort)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		var failedID, siblingID, grandID, outsideID ID
		failed := taskDefinition(t, "task.failfast.failed", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if task.State.Checkpoint["phase"] == "finish" {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "declined"}, Result: &TaskValue{Present: true, Value: nil}}}, nil
				})
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				grandID, err = tx.CreateTask(grand, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish"}}, nil
			})
		})
		opts := failed.options
		opts.Phases = map[string]TaskPhase{"work": failed.options.Phases["work"], "finish": failed.options.Phases["work"]}
		var err error
		failed, err = DefineTask(opts)
		if err != nil {
			t.Fatal(err)
		}
		var outcomes []TaskOutcome
		parent := taskDefinition(t, "task.failfast.parent", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if task.State.Checkpoint["phase"] == "join" {
				var err error
				outcomes, err = r.Outcomes(ctx, []ID{failedID, siblingID})
				if err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				failedID, err = tx.CreateTask(failed, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				siblingID, err = tx.CreateTask(sibling, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				outsideID, err = tx.CreateTask(outside, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{failedID, siblingID}, Policy: "failFast"}, nil
			})
		})
		opts = parent.options
		opts.Phases = map[string]TaskPhase{"work": parent.options.Phases["work"], "join": parent.options.Phases["work"]}
		parent, err = DefineTask(opts)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, parent, failed, sibling, grand, outside)
		cleanupTaskGates(t, releaseGrand, releaseOutside, returnOutside)
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, grandAbort)
		awaitTaskSignal(t, siblingAbort)
		awaitTaskSignal(t, outsideEntered)
		record := observeTaskState(t, h, failedID, "completing")
		if record.State.Outcome.Status != "failed" {
			t.Fatal(record)
		}
		if record, _, err := h.Task(bg, id); err != nil || record.AbortRequested || record.State.Status != "waiting" {
			t.Fatal("failFast marked/resumed parent early", record, err)
		}
		releaseTaskGate(releaseGrand)
		waitPublicTask(t, h, failedID)
		record = observeTaskState(t, h, id, "completing")
		if record.AbortRequested || record.State.Outcome.Status != "completed" {
			t.Fatal("parent final outcome did not Hold for outside child", record)
		}
		for _, member := range []ID{failedID, siblingID, outsideID} {
			child, ok, err := h.Task(bg, member)
			if err != nil || !ok || child.AbortRequested != (member == siblingID) {
				t.Fatal("failFast marked outside On or failing member", child, err)
			}
		}
		if child, _, err := h.Task(bg, outsideID); err != nil || child.State.Status != "running" {
			t.Fatal("outside child not live", child, err)
		}
		releaseTaskGate(releaseOutside)
		awaitTaskSignal(t, outsideTerminal)
		if record, _, err := h.Task(bg, id); err != nil || record.State.Status != "completing" {
			t.Fatal("parent skipped terminal-but-unreturned outside host", record, err)
		}
		releaseTaskGate(returnOutside)
		awaitTaskSignal(t, outsideRuntime.done)
		record = waitPublicTask(t, h, id)
		if outsideAborts.Load() != 0 {
			t.Fatal("outside child was aborted", outsideAborts.Load())
		}
		if record.State.Outcome.Status != "completed" || len(outcomes) != 2 || outcomes[0].Status != "failed" || outcomes[1].Status != "aborted" {
			t.Fatal("held failure join", record, outcomes)
		}
		waitPublicTask(t, h, grandID)
	})
}

func TestTaskOwnershipBackgroundBoundaryAndTerminalOwnerNoCascade(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		gate := make(chan struct{})
		started := make(chan struct{})
		t.Cleanup(func() {
			select {
			case <-gate:
			default:
				close(gate)
			}
		})
		live := taskDefinition(t, "task.background.live", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		done := taskDefinition(t, "task.background.done", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarness(t, b.store, live, done)
		cleanupTaskGates(t, gate)
		background := createPublicTask(t, h, done, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		var childConversation ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			childConversation, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: childConversation, Owner: background})
		})
		if err != nil {
			t.Fatal(err)
		}
		waitPublicTask(t, h, background)
		var child ID
		_, err = h.CommitTasks(bg, childConversation, func(tx *Tx) error {
			var err error
			child, err = tx.CreateTask(live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := h.WaitForIdle(ctx); err != nil {
			t.Fatal("terminal background owner boundary lost", err)
		}
		rootHandle, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := rootHandle.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		record, _, err := h.Task(bg, child)
		if err != nil || record.AbortRequested || record.State.Status != "running" {
			t.Fatal("terminal owner cascaded", record, err)
		}
		if err := rootHandle.AbortWithOptions(ctx, ConversationAbortOptions{Background: true}); err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, h, child); record.State.Outcome.Status != "aborted" {
			t.Fatal(record)
		}
	})
}

func TestTaskOwnershipRejectFinishingChildButOwnedConversationWorkHolds(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		childStarted := make(chan struct{})
		release := make(chan struct{})
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		child := taskDefinition(t, "task.overlay.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(childStarted)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		rejected := make(chan struct{})
		var conversation ID
		var childID ID
		var attempts atomic.Int64
		parent := taskDefinition(t, "task.overlay.parent", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if task.State.Checkpoint["phase"] == "finish" {
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					var err error
					childID, err = tx.CreateTask(child, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "conversation"}})
					if err != nil {
						return nil, err
					}
					return taskDone("held"), nil
				})
			}
			attempts.Add(1)
			err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				if _, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}}); err != nil {
					return nil, err
				}
				return taskDone("invalid"), nil
			})
			if err == nil {
				return errors.New("finishing parent child admitted")
			}
			close(rejected)
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				conversation, err = tx.MintID()
				if err != nil {
					return nil, err
				}
				if err := tx.CreateConversation(Conversation{ID: conversation, Owner: r.TaskID()}); err != nil {
					return nil, err
				}
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish"}}, nil
			})
		})
		options := parent.options
		options.Phases = map[string]TaskPhase{"work": parent.options.Phases["work"], "finish": parent.options.Phases["work"]}
		var err error
		parent, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, parent, child)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, rejected)
		awaitTaskSignal(t, childStarted)
		record := observeTaskState(t, h, id, "completing")
		if record.State.Outcome.Result.Value != "held" {
			t.Fatal("outcome lost", record)
		}
		if _, err := h.AbortTask(bg, id); err != nil {
			t.Fatal(err)
		}
		close(release)
		waitPublicTask(t, h, childID)
		record = waitPublicTask(t, h, id)
		if record.State.Outcome.Result.Value != "held" || !record.AbortRequested || attempts.Load() != 1 {
			t.Fatal("hold changed", record)
		}
	})
}

func TestTaskOwnershipWaitingUnionRejectsAndAllSettledForeignOrder(t *testing.T) {
	for _, variant := range []string{"self", "missing", "direct-owner", "indirect-owner", "failFast-foreign", "foreign-order", "empty"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var parentID, rootOwner, childOwner, first, second ID
				var outcomes []TaskOutcome
				var rejected atomic.Bool
				finished := taskDefinition(t, "task.waiting.finished", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("foreign"), nil })
				})
				waiting, err := DefineTask(TaskDefinitionOptions{Kind: "task.waiting.union", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "wait"}, nil }, Phases: map[string]TaskPhase{
					"wait": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						members := []ID{second, first}
						policy := "allSettled"
						switch variant {
						case "self":
							members = []ID{r.TaskID()}
						case "missing":
							members = []ID{ID(MaxID)}
						case "direct-owner":
							members = []ID{childOwner}
						case "indirect-owner":
							members = []ID{rootOwner}
						case "failFast-foreign":
							policy = "failFast"
						case "empty":
							members = []ID{}
							policy = "failFast"
						}
						err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: members, Policy: policy}, nil
						})
						if variant == "foreign-order" || variant == "empty" {
							return err
						}
						if err == nil {
							return errors.New("invalid waiting union admitted")
						}
						rejected.Store(true)
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("rejected"), nil })
					},
					"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						var err error
						outcomes, err = r.Outcomes(ctx, []ID{second, first})
						if err != nil {
							return err
						}
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("joined"), nil })
					},
				}, Abort: taskAbort})
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, waiting, finished)
				first = createPublicTask(t, h, finished, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				second = createPublicTask(t, h, finished, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				// Committed foreign terminal outcomes differ and remain in requested
				// order; no handler executes on these already terminal records.
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					for index, id := range []ID{first, second} {
						task, err := copyTask(tx.state.Tasks[id], tx.limits)
						if err != nil {
							return err
						}
						value := "first"
						if index == 1 {
							value = "second"
						}
						task.Status = "done"
						task.Execution.Native.State = *taskDone(value)
						if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				rootOwner = createPublicTask(t, h, finished, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				childOwner = createPublicTask(t, h, finished, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: rootOwner}})
				parentID = createPublicTask(t, h, waiting, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: childOwner}})
				h.session.taskBookkeeping(func() {
					h.scheduler.retryAfter[rootOwner] = ^uint64(0)
					h.scheduler.retryAfter[childOwner] = ^uint64(0)
				})
				record := waitPublicTask(t, h, parentID)
				if record.State.Outcome.Status != "completed" {
					t.Fatal(record)
				}
				if variant == "foreign-order" || variant == "empty" {
					if len(outcomes) != 2 || outcomes[0].Result.Value != "second" || outcomes[1].Result.Value != "first" {
						t.Fatal("foreign outcome order", outcomes)
					}
				} else if !rejected.Load() {
					t.Fatal("negative wait rejection missing")
				}
			})
		})
	}
}

func TestTaskOwnershipAllSettledEveryOutcomeLiveBarrier(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		gates := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})}
		started := make(chan *TaskRuntime, 4)
		abortLog := make(chan ID, 5)
		var members []ID
		var observed []TaskOutcome
		definitions := []*TaskDefinition{}
		for index, name := range []string{"completed", "failed", "faulted", "aborted", "orphaned"} {
			index, name := index, name
			def := taskDefinition(t, "task.allsettled."+name, func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if index == 4 {
					return errors.New("unregistered orphan dispatched")
				}
				started <- r
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-gates[index]:
				}
				if name == "faulted" {
					return errors.New("actual child handler failure")
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					if name == "failed" {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "rejected"}}}, nil
					}
					return taskDone(name), nil
				})
			})
			options := def.options
			options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				abortLog <- r.TaskID()
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "explicit"}}, nil
				})
			}
			var err error
			def, err = DefineTask(options)
			if err != nil {
				t.Fatal(err)
			}
			definitions = append(definitions, def)
		}
		parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.allsettled.parent", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil }, Phases: map[string]TaskPhase{
			"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					for _, def := range definitions {
						id, err := tx.CreateTask(def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
						if err != nil {
							return nil, err
						}
						members = append(members, id)
					}
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: members, Policy: "allSettled"}, nil
				})
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				var err error
				observed, err = r.Outcomes(ctx, members)
				if err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("joined"), nil })
			},
		}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, append([]*TaskDefinition{parent}, definitions[:4]...)...)
		cleanupTaskGates(t, gates...)
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		runtimes := map[ID]*TaskRuntime{}
		for index := 0; index < 4; index++ {
			select {
			case r := <-started:
				runtimes[r.TaskID()] = r
			case <-time.After(3 * time.Second):
				t.Fatal("allSettled actual child starts missing")
			}
		}
		for _, index := range []int{1, 2, 0} {
			releaseTaskGate(gates[index])
			waitPublicTask(t, h, members[index])
			awaitTaskSignal(t, runtimes[members[index]].done)
			if record, _, err := h.Task(bg, id); err != nil || record.State.Status != "waiting" || record.AbortRequested {
				t.Fatal("allSettled resumed/marked before every member", record, err)
			}
			for _, member := range members {
				if record, _, err := h.Task(bg, member); err != nil || record.AbortRequested {
					t.Fatal("allSettled marked sibling", record, err)
				}
			}
		}
		if result, err := h.AbortTask(bg, members[3]); err != nil || result != "marked" {
			t.Fatal(result, err)
		}
		if result, err := h.AbortTask(bg, members[4]); err != nil || result != "marked" {
			t.Fatal(result, err)
		}
		record := waitPublicTask(t, h, id)
		if record.AbortRequested || record.State.Outcome.Status != "completed" || len(observed) != 5 {
			t.Fatal("allSettled parent", record, observed)
		}
		for index, status := range []string{"completed", "failed", "faulted", "aborted", "orphaned"} {
			if observed[index].Status != status {
				t.Fatal("ordered outcomes", observed)
			}
		}
		if len(abortLog) != 1 || <-abortLog != members[3] {
			t.Fatal("allSettled abort log includes nonexplicit sibling")
		}
	})
}

func TestTaskOwnershipFailFastFailedAndFaultedChildren(t *testing.T) {
	for _, ending := range []string{"failed", "faulted"} {
		t.Run(ending, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				releaseFailure := make(chan struct{})
				started := make(chan *TaskRuntime, 4)
				abortLog := make(chan ID, 5)
				var members []ID
				var observed []TaskOutcome
				child := taskDefinition(t, "task.failfast.child", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
					started <- r
					if task.Input.Value == "failure" {
						<-releaseFailure
						if ending == "faulted" {
							return errors.New("real phase fault")
						}
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "failed"}}}, nil
						})
					}
					<-ctx.Done()
					return ctx.Err()
				})
				options := child.options
				options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					abortLog <- r.TaskID()
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				var err error
				child, err = DefineTask(options)
				if err != nil {
					t.Fatal(err)
				}
				parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.failfast.parent", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil }, Phases: map[string]TaskPhase{
					"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
							for index := 0; index < 4; index++ {
								input := "sibling"
								if index == 1 {
									input = "failure"
								}
								id, err := tx.CreateTask(child, input, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
								if err != nil {
									return nil, err
								}
								members = append(members, id)
							}
							return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: members, Policy: "failFast"}, nil
						})
					},
					"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						var err error
						observed, err = r.Outcomes(ctx, members)
						if err != nil {
							return err
						}
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
					},
				}, Abort: taskAbort})
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, parent, child)
				cleanupTaskGates(t, releaseFailure)
				id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				runtimes := []*TaskRuntime{}
				for index := 0; index < 4; index++ {
					select {
					case r := <-started:
						runtimes = append(runtimes, r)
					case <-time.After(3 * time.Second):
						t.Fatal("failFast child admission")
					}
				}
				releaseTaskGate(releaseFailure)
				record := waitPublicTask(t, h, id)
				for _, r := range runtimes {
					awaitTaskSignal(t, r.done)
				}
				if record.AbortRequested || record.State.Outcome.Status != "completed" || len(observed) != 4 {
					t.Fatal("failFast parent", record, observed)
				}
				for index, outcome := range observed {
					expected := "aborted"
					if index == 1 {
						expected = ending
					}
					if outcome.Status != expected {
						t.Fatal("failFast ordered outcomes", observed)
					}
				}
				if len(abortLog) != 3 {
					t.Fatal("failFast abort count", len(abortLog))
				}
				seen := map[ID]bool{}
				for len(abortLog) > 0 {
					member := <-abortLog
					if member == members[1] || seen[member] {
						t.Fatal("failFast aborted failing member or repeated", member)
					}
					seen[member] = true
				}
				for _, index := range []int{0, 2, 3} {
					if !seen[members[index]] {
						t.Fatal("missing sibling abort", members[index])
					}
				}
			})
		})
	}
}

func TestTaskOwnershipDirectBackgroundAbortStopsAtNestedBackground(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		started := make(chan ID, 4)
		release := make(chan struct{})
		var aborted atomic.Int64
		live := taskDefinition(t, "task.background.nested", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			started <- r.TaskID()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return r.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("released"), nil })
			}
		})
		options := live.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborted.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		var err error
		live, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, live)
		cleanupTaskGates(t, release)
		background := createPublicTask(t, h, live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		ordinary := createPublicTask(t, h, live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: background}})
		var ownedConversation ID
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			ownedConversation, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: ownedConversation, Owner: background})
		})
		if err != nil {
			t.Fatal(err)
		}
		var nested, nestedChild ID
		_, err = h.CommitTasks(bg, ownedConversation, func(tx *Tx) error {
			var err error
			nested, err = tx.CreateTask(live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
			if err != nil {
				return err
			}
			nestedChild, err = tx.CreateTask(live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: nested}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		for k := 0; k < 4; k++ {
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("nested background actual starts missing")
			}
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		rootHandle, _ := h.Conversation(bg, 1)
		if err := rootHandle.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{background, ordinary, nested, nestedChild} {
			record, _, err := h.Task(bg, id)
			if err != nil || record.AbortRequested {
				t.Fatal("ordinary root abort crossed background boundary", id, record, err)
			}
		}
		if _, err := h.AbortTask(ctx, background); err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, h, background); record.State.Outcome.Status != "aborted" {
			t.Fatal(record)
		}
		if record := waitPublicTask(t, h, ordinary); record.State.Outcome.Status != "aborted" {
			t.Fatal(record)
		}
		for _, id := range []ID{nested, nestedChild} {
			record, _, err := h.Task(bg, id)
			if err != nil || record.AbortRequested || record.State.Status != "running" {
				t.Fatal("direct background abort crossed nested background boundary", record, err)
			}
		}
		if aborted.Load() != 2 {
			t.Fatal("ordinary descendant/background abort count", aborted.Load())
		}
		if err := rootHandle.AbortWithOptions(ctx, ConversationAbortOptions{Background: true}); err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{nested, nestedChild} {
			if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "aborted" {
				t.Fatal(record)
			}
		}
		if aborted.Load() != 4 {
			t.Fatal("explicit background true snapshot missed nested subtree", aborted.Load())
		}
	})
}

func TestTaskOwnershipBackgroundTrueUsesAdmissionSnapshot(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		started := make(chan ID, 2)
		oldAbortEntered, releaseOldAbort, releaseFuture := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var oldID, futureID ID
		var aborts atomic.Int64
		old := taskDefinition(t, "task.background.snapshot.old", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			started <- r.TaskID()
			<-ctx.Done()
			return ctx.Err()
		})
		options := old.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			close(oldAbortEntered)
			<-releaseOldAbort
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		var err error
		old, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		future := taskDefinition(t, "task.background.snapshot.future", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			started <- r.TaskID()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-releaseFuture:
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			}
		})
		h := taskTestHarness(t, b.store, old, future)
		cleanupTaskGates(t, releaseOldAbort, releaseFuture)
		oldID = createPublicTask(t, h, old, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		select {
		case id := <-started:
			if id != oldID {
				t.Fatal(id)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("old background start missing")
		}
		rootHandle, _ := h.Conversation(bg, 1)
		result := make(chan error, 1)
		go func() { result <- rootHandle.AbortWithOptions(bg, ConversationAbortOptions{Background: true}) }()
		awaitTaskSignal(t, oldAbortEntered)
		futureID = createPublicTask(t, h, future, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		select {
		case id := <-started:
			if id != futureID {
				t.Fatal(id)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("later admitted background start missing")
		}
		releaseTaskGate(releaseOldAbort)
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("background abort waited work outside admission snapshot")
		}
		record, _, err := h.Task(bg, futureID)
		if err != nil || record.AbortRequested || record.State.Status != "running" || aborts.Load() != 1 {
			t.Fatal("background abort touched later work", record, err, aborts.Load())
		}
		releaseTaskGate(releaseFuture)
		waitPublicTask(t, h, futureID)
	})
}

func TestTaskOwnershipTaskDocumentsRetainedHeldAndSameBatchTerminalRetired(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct-final", true: "held-final"}[held], func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				childEntered, releaseChild, parentDecided := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var taskDoc, entryID, childID ID
				var parentRuntime *TaskRuntime
				child := taskDefinition(t, "task.documents.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(childEntered)
					<-releaseChild
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				parent := taskDefinition(t, "task.documents.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					parentRuntime = r
					if _, err := r.MemoCandidate(ctx, "retained", JSON{"memo": "owned"}); err != nil {
						return err
					}
					if held {
						if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
							var err error
							childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
							return nil, err
						}); err != nil {
							return err
						}
					}
					if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						var err error
						taskDoc, err = tx.MintID()
						if err != nil {
							return nil, err
						}
						if _, err := tx.CreateDocument(Document{ID: taskDoc, Scope: "task", Owner: r.TaskID(), Kind: "app.samebatch-lifetime", Version: 1, Value: JSON{"kept": "value"}}); err != nil {
							return nil, err
						}
						entryID, err = tx.MintID()
						if err != nil {
							return nil, err
						}
						if err := tx.AppendEntry(Entry{ID: entryID, Conversation: r.ConversationID(), Kind: "task.document-result", Value: JSON{"document": taskDoc}}); err != nil {
							return nil, err
						}
						return taskDone(JSON{"entry": entryID}), nil
					}); err != nil {
						return err
					}
					close(parentDecided)
					return nil
				})
				h := taskTestHarness(t, b.store, parent, child)
				cleanupTaskGates(t, releaseChild)
				id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, parentDecided)
				awaitTaskSignal(t, parentRuntime.done)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if state.Tasks[id].Execution.Native.Memos != nil || state.Entries[entryID].ByTask != id || state.Entries[entryID].Seq == 0 {
					t.Fatal("same deciding memo/result/entry placement")
				}
				if held {
					awaitTaskSignal(t, childEntered)
					if state.Tasks[id].Status != "completing" || state.Documents[taskDoc].Retired {
						t.Fatal("held task document retired before join")
					}
					conversation, err := h.Conversation(bg, 1)
					if err != nil {
						t.Fatal(err)
					}
					caller, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					waited := make(chan error, 1)
					idle := make(chan error, 1)
					go func() {
						record, err := h.WaitForTask(caller, id)
						if err == nil && record.State.Outcome.Status != "completed" {
							err = errors.New("held parent waiter returned wrong outcome")
						}
						waited <- err
					}()
					go func() { idle <- conversation.WaitForIdle(caller) }()
					for {
						parentRegistered, idleRegistered := false, false
						h.session.taskBookkeeping(func() {
							for wait := range h.scheduler.waiters {
								if wait.binding == nil && wait.target == id {
									parentRegistered = true
								}
								if wait.binding == nil && wait.idleScope != nil && *wait.idleScope == 1 {
									idleRegistered = true
								}
							}
						})
						if parentRegistered && idleRegistered {
							break
						}
						select {
						case <-caller.Done():
							t.Fatal("held waiter/idle not admitted")
						default:
							runtime.Gosched()
						}
					}
					select {
					case err := <-waited:
						t.Fatal("held waiter returned early", err)
					default:
					}
					select {
					case err := <-idle:
						t.Fatal("held root idle returned early", err)
					default:
					}
					inspection, err := h.InspectTasks(bg)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, item := range inspection.Tasks {
						if item.Record.ID == id {
							found = item.Kind == "completing"
						}
					}
					if !found {
						t.Fatal("held parent inspection not completing", inspection)
					}
					query := b.store.(DocumentHistoryStorage)
					if doc, present, err := query.DocumentAt(bg, taskDoc, CurrentDocumentPoint()); err != nil || !present || doc.Value["kept"] != "value" {
						t.Fatal("held document not current", doc, present, err)
					}
					releaseTaskGate(releaseChild)
					for _, result := range []<-chan error{waited, idle} {
						select {
						case err := <-result:
							if err != nil {
								t.Fatal(err)
							}
						case <-caller.Done():
							t.Fatal("held waiter/idle did not settle", caller.Err())
						}
					}
					if _, present, err := query.FindDocument(bg, DocumentAddress{Scope: "task", Owner: id, Kind: "app.samebatch-lifetime"}, CurrentDocumentPoint()); err != nil || present {
						t.Fatal("settled task document current", present, err)
					}
					waitPublicTask(t, h, childID)
				} else if state.Tasks[id].Status != "done" || !state.Documents[taskDoc].Retired {
					t.Fatal("samebatch direct terminal document not retired")
				}
				record := waitPublicTask(t, h, id)
				if record.State.Outcome.Status != "completed" || record.Memos != nil {
					t.Fatal(record)
				}
				final, err := h.Snapshot(bg)
				if err != nil || !final.Documents[taskDoc].Retired || final.Documents[taskDoc].RetiredAt == 0 || final.Documents[taskDoc].RetiredAt > final.Seq {
					t.Fatal("taskdoc terminal cleanup placement", final.Documents[taskDoc], err)
				}
			})
		})
	}
}

func TestTaskOwnershipPublicRawTypedDocumentOwnerAdmission(t *testing.T) {
	for _, ownerKind := range []string{"native-terminal", "builtin-terminal", "missing-task", "missing-conversation"} {
		for _, acquisition := range []string{"raw", "typed"} {
			t.Run(ownerKind+"/"+acquisition, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					ownerID := mint(t, b.store)
					if ownerKind == "native-terminal" {
						owner := foundationTask(ownerID, JSON{"kept": "input"})
						owner.Status, owner.Execution.Native.State = "done", *taskDone("settled")
						apply(t, b.store, Write{Op: "put-task", Task: &owner})
					} else if ownerKind == "builtin-terminal" {
						// Metadata-bearing terminal DTO, not a production scheduler-tool
						// fault/orphan fixture. Receipt policy is not under test here.
						owner := Task{ID: ownerID, Conversation: 1, Kind: "pi.tool", Status: "done", Checkpoint: JSON{}, Execution: &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{}}}
						apply(t, b.store, Write{Op: "put-task", Task: &owner})
					}
					docID := mint(t, b.store) // Allocate before the rejection sequence witness.
					h := taskTestHarness(t, b.store)
					scope := "task"
					if ownerKind == "missing-conversation" {
						scope = "conversation"
					}
					var initializers atomic.Int64
					definition, err := DefineDocument(DefinitionOptions{Kind: "app.owner-admission", Scope: scope, Version: 1, Initial: func(JSON) (JSON, error) { initializers.Add(1); return JSON{"value": "initial"}, nil }})
					if err != nil {
						t.Fatal(err)
					}
					before, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
						if acquisition == "typed" {
							_, err := tx.AcquireDocument(definition, ownerID, nil, nil)
							return err
						}
						_, err := tx.CreateDocument(Document{ID: docID, Scope: scope, Owner: ownerID, Kind: "app.owner-admission", Version: 1, Value: JSON{"value": "raw"}})
						return err
					})
					var rejected *StorageRejected
					if !errors.As(err, &rejected) {
						t.Fatal("invalid document owner acquired", err)
					}
					after, err := h.Snapshot(bg)
					if err != nil || after.Seq != before.Seq || after.HighWater != before.HighWater || len(after.Documents) != len(before.Documents) || initializers.Load() != 0 {
						t.Fatal("rejected owner changed sequence/document/initializer", err)
					}
					query := b.store.(DocumentHistoryStorage)
					if _, found, err := query.FindDocument(bg, DocumentAddress{Scope: scope, Owner: ownerID, Kind: "app.owner-admission"}, CurrentDocumentPoint()); err != nil || found {
						t.Fatal("invalid owner current document survived", found, err)
					}
					if ownerKind == "native-terminal" && !equalTaskValue(before.Tasks[ownerID].Execution.Native.Input, after.Tasks[ownerID].Execution.Native.Input, h.session.limits) {
						t.Fatal("document rejection changed owner input")
					}
				})
			})
		}
	}
}

func TestTaskOwnershipTerminalCandidateDocumentOverlayAndPublication(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ownerID, committedID, newRawID, lateID := mint(t, b.store), mint(t, b.store), mint(t, b.store), mint(t, b.store)
		owner := foundationTask(ownerID, "immutable input")
		apply(t, b.store, Write{Op: "put-task", Task: &owner})
		h := taskTestHarness(t, b.store)
		definition, err := DefineDocument(DefinitionOptions{Kind: "app.overlay-family", Scope: "task", Family: true, Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"value": "initial"}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		oldKey, newKey, lateKey := "committed", "new", "late"
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			_, err := tx.CreateDocument(Document{ID: committedID, Scope: "task", Owner: ownerID, Kind: "app.overlay-family", Family: true, Key: oldKey, Version: 1, Value: JSON{"value": "committed"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		baseline, subscription, err := h.session.SubscribeCommits(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer subscription.Stop()
		frames := make(chan PublicationFrame, 2)
		if err := subscription.Start(func(_ context.Context, frame PublicationFrame) error { frames <- frame; return nil }); err != nil {
			t.Fatal(err)
		}
		var newTypedID ID
		seq, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			acquired, err := tx.AcquireDocument(definition, ownerID, &oldKey, nil)
			if err != nil {
				return err
			}
			created, err := tx.AcquireDocument(definition, ownerID, &newKey, nil)
			if err != nil {
				return err
			}
			newTypedID = created.id
			if err := created.Set(JSON{"value": "created then retired"}); err != nil {
				return err
			}
			if _, err := tx.CreateDocument(Document{ID: newRawID, Scope: "task", Owner: ownerID, Kind: "app.overlay-raw", Version: 1, Value: JSON{"value": "raw before terminal"}}); err != nil {
				return err
			}
			terminal, err := copyTask(tx.state.Tasks[ownerID], tx.limits)
			if err != nil {
				return err
			}
			terminal.Status, terminal.Execution.Native.State = "done", *taskDone("final")
			// Public task creation is bound here; replacement is the private
			// sealed-adapter staging seam used to inspect a terminal overlay.
			if err := tx.stage(Write{Op: "put-task", Task: &terminal}); err != nil {
				return err
			}
			if err := tx.stage(Write{Op: "put-task", Task: &owner}); err == nil {
				return errors.New("terminal candidate reverted to live")
			}
			for _, key := range []*string{&oldKey, &lateKey} {
				if _, err := tx.AcquireDocument(definition, ownerID, key, nil); err == nil {
					return errors.New("typed acquisition after terminal candidate succeeded")
				}
			}
			if _, err := tx.CreateDocument(Document{ID: lateID, Scope: "task", Owner: ownerID, Kind: "app.overlay-late", Version: 1, Value: JSON{}}); err == nil {
				return errors.New("raw acquisition after terminal candidate succeeded")
			}
			// Existing acquired handle still edits; finalisation retires its
			// resulting content together with new-before-terminal documents.
			return acquired.Set(JSON{"value": "final edit through prior handle"})
		})
		if err != nil || seq != baseline.Seq+1 {
			t.Fatal("terminal overlay admission", seq, err)
		}
		var frame PublicationFrame
		select {
		case frame = <-frames:
		case <-time.After(3 * time.Second):
			t.Fatal("terminal retirement publication missing")
		}
		if frame.Snapshot != nil || frame.Publication.Seq != seq || len(frame.Publication.Documents) != 3 {
			t.Fatal("terminal retirement publication batch", frame)
		}
		publishedIDs := map[ID]bool{}
		for _, change := range frame.Publication.Documents {
			if !change.Record.Retired || change.Record.Scope != "task" || change.Record.Owner != ownerID || change.Value != nil || len(change.Ops) != 0 {
				t.Fatal("terminal document publication", change)
			}
			publishedIDs[change.Record.ID] = true
		}
		if !publishedIDs[committedID] || !publishedIDs[newTypedID] || !publishedIDs[newRawID] {
			t.Fatal("retirement IDs", publishedIDs)
		}
		taskWrites := 0
		for _, write := range frame.Publication.Tables {
			if write.Task != nil && write.Task.ID == ownerID {
				taskWrites++
			}
		}
		if taskWrites != 1 {
			t.Fatal("terminal owner publication count", taskWrites)
		}
		final, err := h.Snapshot(bg)
		if err != nil || final.Tasks[ownerID].Status != "done" || !equalJSONValue(final.Documents[committedID].Value["value"], "final edit through prior handle") {
			t.Fatal("terminal final overlay", err)
		}
		for _, id := range []ID{committedID, newTypedID, newRawID} {
			if !final.Documents[id].Retired || final.Documents[id].RetiredAt != seq {
				t.Fatal("samebatch atomic retirement", final.Documents[id])
			}
		}
		if final.Documents[newTypedID].CreatedAt != seq || final.Documents[newRawID].CreatedAt != seq {
			t.Fatal("new document empty lifetime placement")
		}
		query := b.store.(DocumentHistoryStorage)
		page, err := query.ScanDocuments(bg, DocumentQuery{Scope: "task", Owner: ownerID, At: CurrentDocumentPoint()}, 10, "")
		if err != nil || len(page.Items) != 0 {
			t.Fatal("terminal owner current taskdocs", page, err)
		}
		for _, key := range []string{oldKey, newKey, lateKey} {
			if _, found, err := query.FindDocument(bg, DocumentAddress{Scope: "task", Owner: ownerID, Kind: "app.overlay-family", Family: true, Key: key}, CurrentDocumentPoint()); err != nil || found {
				t.Fatal("terminal current family doc", found, err)
			}
		}
		before := final.Seq
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error { _, err := tx.AcquireDocument(definition, ownerID, &lateKey, nil); return err })
		if err == nil {
			t.Fatal("typed later terminal acquisition")
		}
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			_, err := tx.CreateDocument(Document{ID: lateID, Scope: "task", Owner: ownerID, Kind: "app.overlay-late", Version: 1, Value: JSON{}})
			return err
		})
		if err == nil {
			t.Fatal("raw later terminal acquisition")
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before || len(after.Documents) != len(final.Documents) {
			t.Fatal("later rejection changed sequence/docs", err)
		}
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			terminal, err := copyTask(tx.state.Tasks[ownerID], tx.limits)
			if err != nil {
				return err
			}
			return tx.stage(Write{Op: "put-task", Task: &terminal})
		})
		if err == nil {
			t.Fatal("already terminal owner replaced")
		}
		// A later real entry publication is an ordered witness that rejected
		// acquisitions/replacements emitted no intervening adoption frames.
		sentinel, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "app.retirement-sentinel", Value: JSON{}})
		})
		if err != nil || sentinel != before+1 {
			t.Fatal("post-reject sentinel sequence", sentinel, err)
		}
		select {
		case next := <-frames:
			if next.Publication.Seq != sentinel || len(next.Publication.Documents) != 0 || len(next.Publication.Tables) != 1 || next.Publication.Tables[0].Entry == nil {
				t.Fatal("rejected acquisition published a batch", next)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("post-reject sentinel publication missing")
		}
	})
}

func TestTaskOwnershipRawAbsentExecutionTerminalDocumentPolicyPreserved(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		id, doc := mint(t, b.store), mint(t, b.store)
		owner := Task{ID: id, Conversation: 1, Kind: "legacy.document-owner", Status: "done", Checkpoint: JSON{}}
		apply(t, b.store, Write{Op: "put-task", Task: &owner})
		h := taskTestHarness(t, b.store)
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			_, err := tx.CreateDocument(Document{ID: doc, Scope: "task", Owner: id, Kind: "app.legacy-taskdoc", Version: 1, Value: JSON{"kept": true}})
			return err
		})
		if err != nil {
			t.Fatal("raw legacy taskdoc policy expanded", err)
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Documents[doc].Retired || state.Tasks[id].Execution != nil {
			t.Fatal("legacy inferred native lifetime", err)
		}
	})
}

func TestTaskOwnershipInspectMarkedOwnedAbortActualReturnPrecedence(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		parentReady, childReady, abortReady := make(chan struct{}), make(chan struct{}), make(chan struct{})
		releaseParent, decideAbort, returnAbort := make(chan struct{}), make(chan struct{}), make(chan struct{})
		parentHost, childHost, abortHost := make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1), make(chan *TaskRuntime, 1)
		var clocks, runs, aborts atomic.Int64
		foreign := taskDefinition(t, "task.inspect.foreign", func(context.Context, TaskRecord, *TaskRuntime) error {
			runs.Add(1)
			return errors.New("inspection dispatched foreign")
		})
		child := taskDefinition(t, "task.inspect.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runs.Add(1)
			childHost <- r
			close(childReady)
			<-ctx.Done()
			return ctx.Err()
		})
		options := child.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			abortHost <- r
			close(abortReady)
			<-decideAbort
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			}); err != nil {
				return err
			}
			<-returnAbort // Durable terminal is not the actual host return.
			return nil
		}
		var err error
		child, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		var foreignID, childID ID
		parent := taskDefinition(t, "task.inspect.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runs.Add(1)
			if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{foreignID}, Policy: "allSettled"}, nil
			}); err != nil {
				return err
			}
			parentHost <- r
			close(parentReady)
			<-releaseParent
			return nil
		})
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { clocks.Add(1); return 1 }}, parent, child, foreign)
		cleanupTaskGates(t, releaseParent, decideAbort, returnAbort)
		foreignID = createPublicTask(t, h, foreign, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		h.session.taskBookkeeping(func() { h.scheduler.retryAfter[foreignID] = ^uint64(0) })
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, parentReady)
		awaitTaskSignal(t, childReady)
		oldParent, oldChild := <-parentHost, <-childHost
		// Pause selection at actual parent-mark adoption so the running
		// precedence/read-only witness cannot race the child's cascade writes.
		var core *storeCore
		if memory, ok := b.store.(*MemoryStorage); ok {
			core = memory.storeCore
		} else {
			core = b.store.(*JournalStorage).storeCore
		}
		var original func(byte, uint64, uint64, []byte) error
		h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
		installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				var commit commitRecord
				if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &commit); err != nil {
					return err
				}
				for _, write := range commit.Writes {
					if write.Task != nil && write.Task.ID == id && taskAborted(*write.Task) {
						h.scheduler.enabled.Store(false)
					}
				}
			}
			if original != nil {
				return original(kind, ordinal, high, payload)
			}
			return nil
		})
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		aborting := make(chan error, 1)
		go func() { _, err := h.AbortTask(bg, id); aborting <- err }()
		for {
			record, _, err := h.Task(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if record.AbortRequested {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("parent mark missing")
			default:
				runtime.Gosched()
			}
		}
		check := func(want string, wantOn []ID) {
			t.Helper()
			before, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			var epoch uint64
			h.session.taskBookkeeping(func() { epoch = h.scheduler.epoch.Load() })
			calls := clocks.Load()
			view, err := h.InspectTasks(bg)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, item := range view.Tasks {
				if item.Record.ID == id {
					found = true
					if item.Kind != want || item.Reason != "" || len(item.On) != len(wantOn) {
						t.Fatal("marked parent inspection", item, want, wantOn)
					}
					for i, member := range wantOn {
						if item.On[i] != member {
							t.Fatal("stale foreign wait not superseded", item)
						}
					}
				}
			}
			if !found {
				t.Fatal("marked parent missing")
			}
			after, err := h.Snapshot(bg)
			if err != nil || after.Seq != before.Seq || clocks.Load() != calls {
				t.Fatal("inspection effects", err)
			}
			h.session.taskBookkeeping(func() {
				if h.scheduler.epoch.Load() != epoch {
					t.Error("inspection donated wake")
				}
			})
		}
		check("running", nil) // Ended but unreturned parent still owns actual permit.
		releaseTaskGate(releaseParent)
		awaitTaskSignal(t, oldParent.done)
		select {
		case err := <-aborting:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("parent actual callerjoin missing")
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, oldChild.done)
		awaitTaskSignal(t, abortReady)
		pickedAbort := <-abortHost
		check("waiting", []ID{childID}) // Foreign On remains live but is superseded.
		// Pause before child decision so ready-after-drain is observed without
		// racing a fresh parent abort reservation. No valid epoch is swallowed.
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		releaseTaskGate(decideAbort)
		for {
			record, _, err := h.Task(ctx, childID)
			if err != nil {
				t.Fatal(err)
			}
			if record.State.Status == "terminal" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("child abort terminal missing")
			default:
				runtime.Gosched()
			}
		}
		check("waiting", []ID{childID}) // Terminal child host has not returned yet.
		releaseTaskGate(returnAbort)
		awaitTaskSignal(t, pickedAbort.done)
		check("ready", nil)
		if runs.Load() != 2 || aborts.Load() != 1 || clocks.Load() != 0 {
			t.Fatal("inspection dispatched/clock effects", runs.Load(), aborts.Load(), clocks.Load())
		}
	})
}

func TestTaskOwnershipInspectPausedReopenNativeBuiltinWaitBeforeDefinitionFit(t *testing.T) {
	for _, origin := range []string{"native", "native-pending", "native-running", "builtin-metadata", "builtin-legacy"} {
		for _, marked := range []bool{false, true} {
			if (origin == "native-pending" || origin == "native-running") && !marked {
				continue
			}
			t.Run(origin+map[bool]string{false: "/ordinary", true: "/marked"}[marked], func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					var effects, clocks, migrations atomic.Int64
					definition := taskDefinition(t, "task.inspect.paused", func(context.Context, TaskRecord, *TaskRuntime) error {
						effects.Add(1)
						return errors.New("paused inspection invoked phase")
					})
					options := definition.options
					options.Version = 2
					options.Migrate = func(input any, checkpoint JSON, _ uint64) (any, JSON, error) {
						migrations.Add(1)
						return input, checkpoint, nil
					}
					var err error
					definition, err = DefineTask(options)
					if err != nil {
						t.Fatal(err)
					}
					parentID, childID, foreignID, doneID := mint(t, b.store), mint(t, b.store), mint(t, b.store), mint(t, b.store)
					parent := foundationTask(parentID, JSON{"input": "kept"})
					parent.Kind = definition.Kind()
					parent.Status = "waiting"
					parent.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{doneID, foreignID}, Policy: "allSettled"}
					if origin == "native-pending" || origin == "native-running" {
						parent.Status = "pending"
						if origin == "native-running" {
							parent.Status = "running"
						}
						parent.Execution.Native.State = TaskState{Status: parent.Status, Checkpoint: JSON{"phase": "work"}}
					}
					if origin == "builtin-metadata" || origin == "builtin-legacy" {
						// Synthetic paused raw tools-phase projection. It is not a
						// real dispatched tool-round/effect or crash fixture.
						cp := generationCheckpoint{Phase: "tools", Children: []ID{doneID, foreignID}, Input: "kept"}
						parent.Kind = "pi.generation"
						parent.Status = "completing"
						parent.Checkpoint, err = dtoObject(cp, DefaultLimits())
						if err != nil {
							t.Fatal(err)
						}
						parent.Execution = nil
						if origin == "builtin-metadata" {
							parent.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{}}
						}
					}
					if marked {
						parent = markTask(parent)
					}
					child, foreign, done := foundationTask(childID, nil), foundationTask(foreignID, nil), foundationTask(doneID, nil)
					child.Owner = parentID
					done.Status = "done"
					done.Execution.Native.State = *taskDone("alreadydone")
					apply(t, b.store, Write{Op: "put-task", Task: &parent}, Write{Op: "put-task", Task: &child}, Write{Op: "put-task", Task: &foreign}, Write{Op: "put-task", Task: &done})
					registry := NewRegistry()
					if _, err := registry.RegisterTask(definition); err != nil {
						t.Fatal(err)
					}
					h, err := Open(bg, b.store, Options{Registry: registry, Now: func() int64 { clocks.Add(1); return 1 }})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := h.Close(bg); err != nil {
							t.Error(err)
						}
					})
					inspect := func(host *Harness, want string, on []ID, reason string) {
						t.Helper()
						before, err := host.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						epoch := host.scheduler.epoch.Load()
						view, err := host.InspectTasks(bg)
						if err != nil || view.Scheduling != "paused" {
							t.Fatal("paused inspection", view, err)
						}
						found := false
						for _, item := range view.Tasks {
							if item.Record.ID == parentID {
								found = true
								if item.Kind != want || item.Reason != reason || item.Migrates || len(item.On) != len(on) {
									t.Fatal("paused parent derived state", item, want, on, reason)
								}
								for i, id := range on {
									if item.On[i] != id {
										t.Fatal("paused wait members", item)
									}
								}
								if item.Record.State.Checkpoint != nil {
									item.Record.State.Checkpoint["phase"] = "caller mutation"
								}
								if len(item.Record.State.On) > 0 {
									item.Record.State.On[0] = ID(MaxID)
								}
								if len(item.On) > 0 {
									item.On[0] = ID(MaxID)
								}
							}
						}
						if !found {
							t.Fatal("paused parent omitted")
						}
						after, err := host.Snapshot(bg)
						if err != nil || after.Seq != before.Seq || host.scheduler.epoch.Load() != epoch || effects.Load() != 0 || clocks.Load() != 0 || migrations.Load() != 0 || !equalTaskValue(&TaskValue{Present: true, Value: before.Tasks[parentID].Checkpoint}, &TaskValue{Present: true, Value: after.Tasks[parentID].Checkpoint}, host.session.limits) {
							t.Fatal("Inspect changed records/callbacks/wake", err)
						}
						if origin == "native" || origin == "native-pending" || origin == "native-running" {
							left, right := before.Tasks[parentID].Execution.Native, after.Tasks[parentID].Execution.Native
							if !equalTaskValue(left.Input, right.Input, host.session.limits) || !equalTaskValue(&TaskValue{Present: true, Value: left.State.Checkpoint}, &TaskValue{Present: true, Value: right.State.Checkpoint}, host.session.limits) || len(left.State.On) != len(right.State.On) {
								t.Fatal("inspection mutation escaped canonical native record")
							}
							for i, member := range left.State.On {
								if right.State.On[i] != member {
									t.Fatal("inspection changed native wait members")
								}
							}
						}
					}
					on := []ID{foreignID}
					if marked {
						on = []ID{childID}
					}
					inspect(h, "waiting", on, "") // No migration or fit result leaks into waiting.
					// Remove the native definition: dependencies still take precedence.
					dispose, err := registry.RegisterTask(definition)
					if err != nil {
						t.Fatal(err)
					}
					dispose()
					inspect(h, "waiting", on, "")
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					second, err := Open(bg, reopenStoreAfterHarnessClose(t, b.store), Options{Registry: registry, Now: func() int64 { clocks.Add(1); return 1 }})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if err := second.Close(bg); err != nil {
							t.Error(err)
						}
					})
					inspect(second, "waiting", on, "")
					// Drain just the authoritative dependency; marked records ignore
					// their still-live foreign On, ordinary records ignore ownedchild.
					_, err = second.CommitTasks(bg, 1, func(tx *Tx) error {
						target := foreignID
						if marked {
							target = childID
						}
						record, err := copyTask(tx.state.Tasks[target], tx.limits)
						if err != nil {
							return err
						}
						record.Status = "done"
						record.Execution.Native.State = *taskDone("drained")
						return tx.stage(Write{Op: "put-task", Task: &record})
					})
					if err != nil {
						t.Fatal(err)
					}
					want, reason := "ready", ""
					if origin == "native" || origin == "native-pending" || origin == "native-running" {
						want = "blocked"
						reason = "missing_task"
					}
					inspect(second, want, nil, reason)
				})
			})
		}
	}
}

func TestTaskOwnershipRunningPhasesWaitOnSubsetsInSequence(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		gates := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
		started := make(chan *TaskRuntime, 3)
		rounds := make(chan int, 3)
		var members []ID
		child := taskDefinition(t, "task.subsets.child", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			value, err := task.Input.Value.(json.Number).Int64()
			if err != nil {
				return err
			}
			index := int(value)
			started <- r
			<-gates[index]
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(index), nil })
		})
		parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.subsets.parent", Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil }, Phases: map[string]TaskPhase{
				"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						for index := 0; index < 3; index++ {
							id, err := tx.CreateTask(child, index, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
							if err != nil {
								return nil, err
							}
							members = append(members, id)
						}
						return &TaskState{Status: "running", Checkpoint: JSON{"phase": "round0"}}, nil
					})
				},
				"round0": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					rounds <- 0
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "round1"}, On: []ID{members[0]}, Policy: "allSettled"}, nil
					})
				},
				"round1": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					rounds <- 1
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "round2"}, On: []ID{members[1], members[2]}, Policy: "failFast"}, nil
					})
				},
				"round2": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					rounds <- 2
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
				},
			}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, parent, child)
		cleanupTaskGates(t, gates...)
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		runtimes := map[ID]*TaskRuntime{}
		for index := 0; index < 3; index++ {
			select {
			case r := <-started:
				runtimes[r.TaskID()] = r
			case <-time.After(3 * time.Second):
				t.Fatal("subset child start")
			}
		}
		first := observeTaskState(t, h, id, "waiting")
		if first.State.Checkpoint["phase"] != "round1" || len(first.State.On) != 1 || first.State.On[0] != members[0] || first.State.Policy != "allSettled" {
			t.Fatal("first subset", first)
		}
		releaseTaskGate(gates[1])
		waitPublicTask(t, h, members[1])
		awaitTaskSignal(t, runtimes[members[1]].done)
		if record, _, err := h.Task(bg, id); err != nil || record.State.Status != "waiting" || record.State.Checkpoint["phase"] != "round1" {
			t.Fatal("outside first subset resumed parent", record, err)
		}
		releaseTaskGate(gates[0])
		waitPublicTask(t, h, members[0])
		awaitTaskSignal(t, runtimes[members[0]].done)
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			record, _, err := h.Task(deadline, id)
			if err != nil {
				t.Fatal(err)
			}
			if record.State.Status == "waiting" && record.State.Checkpoint["phase"] == "round2" {
				if record.State.Policy != "failFast" || len(record.State.On) != 2 || record.State.On[0] != members[1] || record.State.On[1] != members[2] {
					t.Fatal("second subset", record)
				}
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("second subset not adopted")
			default:
				runtime.Gosched()
			}
		}
		releaseTaskGate(gates[2])
		awaitTaskSignal(t, runtimes[members[2]].done)
		record := waitPublicTask(t, h, id)
		if record.AbortRequested || record.State.Outcome.Status != "completed" || len(rounds) != 3 {
			t.Fatal("subset parent", record, len(rounds))
		}
		for index := 0; index < 3; index++ {
			if got := <-rounds; got != index {
				t.Fatal("round order", index, got)
			}
		}
	})
}
