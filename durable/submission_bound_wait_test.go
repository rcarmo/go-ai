package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"testing"
	"time"
)

func TestInvocationSubmissionWaitQueuedPassiveWriteUsesSettlementPublication(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
			backends(t, func(t *testing.T, b backend) {
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
				_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					subID, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.PutSubmission(Submission{ID: subID, Conversation: 1, Type: "write", Status: "pending", Value: JSON{}})
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
