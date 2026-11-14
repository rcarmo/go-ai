package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvocationSubmissionWaitQueuedPassiveWriteUsesSettlementPublication(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered, release := make(chan struct{}), make(chan struct{})
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			close(entered)
			go func() { <-release; ch <- terminal("answer"); close(ch) }()
			return ch
		})
		admitted := make(chan *InvocationSubmission, 1)
		owner := taskDefinition(t, "task.bound.queued.write", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			c, err := r.Conversation(ctx, 1)
			if err != nil {
				return err
			}
			sub, err := c.Submit(ctx, Input{Type: "write", Content: "queued passive"})
			if err != nil {
				return err
			}
			admitted <- sub
			settled, err := sub.Wait(ctx)
			if err != nil {
				return err
			}
			if settled.Submission.Status != "done" {
				return errors.New("passive write not settled")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("published"), nil })
		})
		h := taskTestHarnessOptions(t, b.store, options, owner)
		cleanupTaskGates(t, release)
		root := root(t, h, ref)
		first, err := root.Submit(bg, Input{Content: "held generation"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		var sub *InvocationSubmission
		select {
		case sub = <-admitted:
		case <-time.After(3 * time.Second):
			t.Fatal("passive write not admitted")
		}
		deadline := time.After(3 * time.Second)
		for {
			registered := false
			h.session.taskBookkeeping(func() { registered = len(h.submissionWaiters[sub.ID()]) == 1 })
			if registered {
				break
			}
			select {
			case <-deadline:
				t.Fatal("bound passive wait not registered")
			default:
			}
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Submissions[sub.ID()].Status != "pending" {
			t.Fatal(state.Submissions[sub.ID()], err)
		}
		releaseTaskGate(release)
		waitSubmission(t, first)
		final := waitPublicTask(t, h, id)
		if final.State.Outcome == nil || final.State.Outcome.Status != "completed" {
			t.Fatal(final)
		}
		h.session.taskBookkeeping(func() {
			if len(h.submissionWaiters) != 0 {
				t.Error("bound waiter retained")
			}
		})
	})
}

func TestInvocationSubmissionQueuedStatusAbortAndWaitRejectAfterSeal(t *testing.T) {
	for _, operation := range []string{"status", "abort", "wait"} {
		t.Run(operation, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				entered, release := make(chan struct{}), make(chan struct{})
				var runtime *TaskRuntime
				owner := taskDefinition(t, "task.bound.queued.operations", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
					runtime = r
					close(entered)
					<-release
					return nil
				})
				h := taskTestHarness(t, b.store, owner)
				cleanupTaskGates(t, release)
				var subID ID
				_, err := h.commitTasks(bg, 1, func(tx *Tx) error {
					var err error
					subID, err = tx.mintID()
					if err != nil {
						return err
					}
					return tx.putSubmissionRecord(Submission{ID: subID, Conversation: 1, Type: "write", Status: "pending", Value: JSON{}})
				})
				if err != nil {
					t.Fatal(err)
				}
				id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				bound := &InvocationSubmission{runtime: runtime, handle: &SubmissionHandle{h: h, id: subID}}
				queued := make(chan struct{})
				ctx := &taskAdmissionContext{Context: bg, queued: queued}
				result := make(chan error, 1)
				// The committed deciding callback owns the line until the operation has
				// actually attempted admission; adoption ends its authority before release.
				if err := runtime.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
					go func() {
						var err error
						switch operation {
						case "status":
							_, err = bound.Status(ctx)
						case "abort":
							_, err = bound.Abort(ctx)
						case "wait":
							_, err = bound.Wait(ctx)
						}
						result <- err
					}()
					awaitTaskSignal(t, queued)
					return taskDone("sealed"), nil
				}); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-result:
					if !errors.Is(err, ErrSealed) && !errors.Is(err, context.Canceled) {
						t.Fatal("queued bound operation survived seal", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("bound operation retained after seal")
				}
				state, err := h.Snapshot(bg)
				if err != nil || state.Submissions[subID].Status != "pending" || state.Tasks[id].Status != "done" {
					t.Fatal("sealed operation changed durable state", err)
				}
				h.session.taskBookkeeping(func() {
					if len(h.submissionWaiters) != 0 {
						t.Error("sealed wait registered")
					}
				})
				releaseTaskGate(release)
			})
		})
	}
}

// Literal pinned harness-ownership.test.ts1013-1075: the tool's handles
// expire, but its child's submitted work and records remain durable.
func TestSubmissionToolBoundConversationLifetime(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var handle, missing *InvocationConversation
	var submission *InvocationSubmission
	var ref ModelRef
	registry := NewRegistry()
	err = registry.Register(ToolRegistration{Definition: goai.Tool{Name: "delegate", Description: "Delegates", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "source.delegate", Version: 1, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
		value, err := api.Commit(ctx, func(tx *Tx) (any, error) {
			child, err := tx.CreateConversation(JSON{"ownership": JSON{"kind": "task", "taskId": api.TaskID()}})
			if err != nil {
				return nil, err
			}
			agent, err := builtin(tx, child.ID, "pi.agent")
			if err != nil {
				return nil, err
			}
			configured, err := api.h.agent(AgentChange{Model: ref})
			if err != nil {
				return nil, err
			}
			data, err := dtoObject(configured, tx.limits)
			if err != nil {
				return nil, err
			}
			return child.ID, agent.Set(data)
		})
		if err != nil {
			return ToolResult{}, err
		}
		missing, err = api.Conversation(ctx, 99999)
		if err != nil {
			t.Error("source missing conversation lookup must return no handle without error", err)
		}
		handle, err = api.Conversation(ctx, value.(ID))
		if err != nil {
			return ToolResult{}, err
		}
		submission, err = handle.Submit(ctx, Input{Type: "input", Content: "child task", RequestID: "child"})
		if err != nil {
			return ToolResult{}, err
		}
		settled, err := submission.Wait(ctx)
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Content: settled.Record.Status}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	var options Options
	ref, options = setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		switch calls.Add(1) {
		case 1:
			ch <- toolAnswer("c1", "delegate", JSON{})
		case 2:
			ch <- terminal("child answer")
		default:
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	root := root(t, h, ref)
	first, err := root.Submit(bg, Input{Type: "input", Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, first)
	if err := h.WaitForIdle(bg); err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Error("source missing conversation returned a handle")
	}
	if handle == nil || submission == nil {
		t.Fatal("delegate did not create bound work")
	}
	if _, err := handle.Submit(bg, Input{Type: "input", Content: "again"}); !errors.Is(err, ErrSealed) {
		t.Error("ended submit", err)
	}
	if err := handle.WaitForIdle(bg); !errors.Is(err, ErrSealed) {
		t.Error("ended idle", err)
	}
	if err := handle.Abort(bg); !errors.Is(err, ErrSealed) {
		t.Error("ended abort", err)
	}
	if _, err := submission.Wait(bg); !errors.Is(err, ErrSealed) {
		t.Error("ended submission wait", err)
	}
	child, err := h.Conversation(bg, handle.ID())
	if err != nil || child == nil {
		t.Fatal(child, err)
	}
	page, err := child.Entries(bg, EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, entry := range page.Items {
		if entry.Kind == "pi.user" {
			users++
		}
	}
	if users != 1 {
		t.Error("late operation admitted input", users)
	}
	inspection, err := h.Inspect(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range inspection.Tasks {
		if task.Record.Conversation == handle.ID() {
			t.Error("child task still active", task)
		}
	}
	durable, err := h.Submission(bg, submission.ID())
	if err != nil || durable == nil {
		t.Fatal(durable, err)
	}
	record, err := durable.Status(bg)
	if err != nil || record.Status != "done" {
		t.Fatal(record, err)
	}
}
