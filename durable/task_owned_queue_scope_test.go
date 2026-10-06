package durable

import (
	"context"
	"testing"
	"time"
)

func seedOwnedQueue(t *testing.T, h *Harness, conversation ID, label string) (ID, ID) {
	t.Helper()
	var input, write ID
	_, err := h.CommitTasks(bg, conversation, func(tx *Tx) error {
		var err error
		input, err = tx.MintID()
		if err != nil {
			return err
		}
		generation, err := tx.MintID()
		if err != nil {
			return err
		}
		write, err = tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.PutSubmission(Submission{ID: input, Conversation: conversation, Type: "follow-up", Status: "pending", Value: JSON{"content": label}}); err != nil {
			return err
		}
		checkpoint, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: input, Input: label}, tx.limits)
		if err != nil {
			return err
		}
		if err = tx.PutTask(Task{ID: generation, Conversation: conversation, Kind: "pi.generation", Status: "pending", Checkpoint: checkpoint}); err != nil {
			return err
		}
		if err = tx.PutSubmission(Submission{ID: write, Conversation: conversation, Type: "write", Status: "pending", Value: JSON{"content": "kept write"}}); err != nil {
			return err
		}
		inbox, err := builtin(tx, conversation, "pi.inbox")
		if err != nil {
			return err
		}
		return inbox.Update(func(value JSON) error {
			value["items"] = append(value["items"].([]any), JSON{"id": input, "mode": "followUp", "input": JSON{"content": label}}, JSON{"id": write, "mode": "write"})
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return input, write
}

func TestTaskOwnershipPinnedQueuedInputScopePreservesWritesAndBackground(t *testing.T) {
	for _, scope := range []string{"task", "conversation", "held-failure"} {
		t.Run(scope, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				started := make(chan ID, 4)
				releaseAbort := make(chan struct{})
				cleanupTaskGates(t, releaseAbort)
				live := taskDefinition(t, "task.queue-scope.live", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					started <- r.TaskID()
					<-ctx.Done()
					return ctx.Err()
				})
				live.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					<-releaseAbort
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				h := taskTestHarness(t, b.store, live)
				_, err := h.Root(bg, AgentChange{})
				if err != nil {
					t.Fatal(err)
				}
				owner := createPublicTask(t, h, live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				background := createPublicTask(t, h, live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
				var owned ID
				_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					owned, err = tx.MintID()
					if err != nil {
						return err
					}
					return tx.CreateConversation(Conversation{ID: owned, Owner: owner})
				})
				if err != nil {
					t.Fatal(err)
				}
				child := createPublicTask(t, h, live, nil, TaskOptions{Conversation: owned, Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 3; i++ {
					select {
					case <-started:
					case <-time.After(3 * time.Second):
						t.Fatal("live tree not started")
					}
				}
				h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
				if scope == "held-failure" {
					// A validated deciding seam models a failed owner whose aborting child
					// keeps its outcome held; late queued input must not dispatch below it.
					_, err = h.session.Commit(bg, func(tx *Tx) error {
						state := TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "declined"}}}
						return seedNativeRecoveryState(tx, owner, state, false)
					})
					if err != nil {
						t.Fatal(err)
					}
					if err := h.scheduler.reconcile(); err != nil {
						t.Fatal(err)
					}
				}
				ownInput, ownWrite := seedOwnedQueue(t, h, 1, "own")
				childInput, childWrite := seedOwnedQueue(t, h, owned, "below")
				if scope != "held-failure" {
					_, err = h.session.Commit(bg, func(tx *Tx) error {
						task := markTask(tx.state.Tasks[owner])
						return tx.stage(Write{Op: "put-task", Task: &task})
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if scope == "conversation" {
					_, err = h.session.Commit(bg, func(tx *Tx) error { return h.withdrawScopedInputs(tx, 1, false, nil) })
					if err != nil {
						t.Fatal(err)
					}
				}
				for i := 0; i < 6; i++ {
					if err := h.scheduler.reconcile(); err != nil {
						t.Fatal(err)
					}
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if state.Submissions[childInput].Status != "aborted" || state.Submissions[childWrite].Status != "done" || !taskAborted(state.Tasks[child]) || taskAborted(state.Tasks[background]) {
					t.Fatal("scope lost queue/write/background", state.Submissions, state.Tasks)
				}
				want := "pending"
				if scope == "conversation" {
					want = "aborted"
				}
				if state.Submissions[ownInput].Status != want {
					t.Fatal("own conversation input scope", state.Submissions[ownInput], want)
				}
				wantWrite := "pending"
				if scope == "conversation" {
					wantWrite = "done"
				}
				if state.Submissions[ownWrite].Status != wantWrite {
					t.Fatal("write withdrawn", state.Submissions[ownWrite])
				}
				releaseTaskGate(releaseAbort)
				// Close cancels still-protected background code; no queued input needs to
				// enable a model or broaden this controlled scope assertion.
			})
		})
	}
}
