package durable

import (
	"context"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func createQueuedOwnershipTree(t *testing.T, root *ConversationHandle, ownerDef, innerDef *TaskDefinition) (owner, inner, child ID) {
	t.Helper()
	_, err := root.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		owner, err = tx.CreateTask(ownerDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err != nil {
			return nil, err
		}
		record, err := tx.CreateConversation(JSON{"ownership": JSON{"kind": "task", "taskId": owner}})
		if err != nil {
			return nil, err
		}
		child = record.ID
		inner, err = tx.CreateTask(innerDef, nil, TaskOptions{Conversation: child, Ownership: TaskOwnership{Kind: "conversation"}})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

// Like busy() in the pinned tests, use a real task as the temporary run
// owner, without starting a model request that could consume the queue.
func setQueuedOwnershipRun(t *testing.T, c *ConversationHandle, task ID) {
	t.Helper()
	_, err := c.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(LiveDoc(), c.ID(), nil, nil)
		if err != nil {
			return nil, err
		}
		live, err := doc.Get()
		if err != nil {
			return nil, err
		}
		live["run"] = JSON{"taskId": task, "inputs": []any{}, "turns": 0}
		return nil, doc.Set(live)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func requireQueuedOwnershipStatus(t *testing.T, h *Harness, id ID, want string) {
	t.Helper()
	sub, err := h.Submission(bg, id)
	if err != nil || sub == nil {
		t.Fatal(id, sub, err)
	}
	if record, err := sub.Status(bg); err != nil || record.ID != id || record.Status != want {
		t.Fatalf("submission %d status=%q err=%v, want %q", id, record.Status, err, want)
	}
}

// harness-ownership.test.ts:695-711. Only the input below the explicitly
// aborted owner is withdrawn; its own conversation and the write keep queues.
func TestSourceV160TaskAbortWithdrawsOnlyInputsBelowOwner(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		hold := taskDefinition(t, "v160.queue-hold", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			<-ctx.Done()
			return ctx.Err()
		})
		h := taskTestHarness(t, b.store, hold)
		root, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		owner, inner, child := createQueuedOwnershipTree(t, root, hold, hold)
		c, err := h.Conversation(bg, child)
		if err != nil || c == nil {
			t.Fatal(c, err)
		}
		setQueuedOwnershipRun(t, root, owner)
		setQueuedOwnershipRun(t, c, inner)
		own, err := root.Submit(bg, Input{Type: "input", Content: "own"})
		if err != nil {
			t.Fatal(err)
		}
		below, err := c.Submit(bg, Input{Type: "input", Content: "below"})
		if err != nil {
			t.Fatal(err)
		}
		note := "w"
		write, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "note", Model: []MessageReceipt{{Role: goai.RoleUser, ContentText: &note}}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{own.ID(), below.ID(), write.ID()} {
			requireQueuedOwnershipStatus(t, h, id, "queued")
		}
		if mark, err := h.AbortTask(bg, owner); err != nil || mark != "marked" {
			t.Fatal(mark, err)
		}
		waitPublicTask(t, h, inner)
		requireQueuedOwnershipStatus(t, h, own.ID(), "queued")
		requireQueuedOwnershipStatus(t, h, below.ID(), "unanswered")
		requireQueuedOwnershipStatus(t, h, write.ID(), "queued")
	})
}

// harness-ownership.test.ts:804-817. Let the original cascade reach the
// inner abort host, then submit and settle before releasing that host.
func TestSourceV160TaskAbortWithdrawsLateInputBelowHeldFailure(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		fail, abortEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		ownerDef := taskDefinition(t, "v160.queue-owner", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-fail:
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "gate failed"}}}, nil
			})
		})
		innerBase := taskDefinition(t, "v160.queue-inner", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			<-ctx.Done()
			return ctx.Err()
		})
		options := innerBase.options
		options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(abortEntered)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		innerDef, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, ownerDef, innerDef)
		defer releaseTaskGate(release)
		root, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		owner, inner, child := createQueuedOwnershipTree(t, root, ownerDef, innerDef)
		c, err := h.Conversation(bg, child)
		if err != nil || c == nil {
			t.Fatal(c, err)
		}
		setQueuedOwnershipRun(t, c, inner)
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		close(fail)
		awaitTaskSignal(t, abortEntered)
		if task, found, err := h.Task(bg, owner); err != nil || !found || task.State.Status != "completing" {
			t.Fatal("owner not held after failure", task, found, err)
		}
		if task, found, err := h.Task(bg, inner); err != nil || !found || !task.AbortRequested || task.State.Status == "terminal" {
			t.Fatal("descendant abort not held", task, found, err)
		}
		late, err := c.Submit(bg, Input{Type: "input", Content: "late"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		result, err := late.Wait(ctx)
		record := result.Record
		if err != nil || record.ID != late.ID() || record.Status != "unanswered" || record.Reason != "aborted" {
			t.Fatalf("late input before releasing inner abort: %+v, %v; want unanswered/aborted", record, err)
		}
		close(release)
		settled := waitPublicTask(t, h, owner)
		if settled.State.Outcome == nil || settled.State.Outcome.Status != "failed" {
			t.Fatal("owner outcome", settled)
		}
	})
}
