package durable

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestTaskOwnershipPinnedBottomUpAcrossTaskAndConversationEdges(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var bID, xID ID
		log := []string{}
		x := taskDefinition(t, "task.edge.x", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(entered)
			<-release
			return ctx.Err()
		})
		x.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			log = append(log, "x")
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "x"}}, nil
			})
		}
		middle := taskDefinition(t, "task.edge.b", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			conversation, err := r.CreateOwnedConversation(ctx, "edge", nil)
			if err != nil {
				return err
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				xID, err = tx.CreateTask(x, nil, TaskOptions{Conversation: conversation.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{xID}, Policy: "allSettled"}, nil
			})
		})
		middle.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			child, ok, err := r.Task(ctx, xID)
			if err != nil || !ok || child.State.Status != "terminal" {
				t.Error("conversation child still live", child, err)
			}
			log = append(log, "b")
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "b"}}, nil
			})
		}
		top := taskDefinition(t, "task.edge.a", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				bID, err = tx.CreateTask(middle, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{bID}, Policy: "allSettled"}, nil
			})
		})
		top.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			child, ok, err := r.Task(ctx, bID)
			if err != nil || !ok || child.State.Status != "terminal" {
				t.Error(child, err)
			}
			log = append(log, "a")
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "a"}}, nil
			})
		}
		h := taskTestHarness(t, b.store, top, middle, x)
		cleanupTaskGates(t, release)
		if _, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}}); err != nil {
			t.Fatal(err)
		}
		topID := createPublicTask(t, h, top, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		caller, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() { _, err := h.AbortTask(caller, topID); result <- err }()
		for {
			state, err := h.Snapshot(caller)
			if err != nil {
				t.Fatal(err)
			}
			if taskAborted(state.Tasks[xID]) {
				break
			}
		}
		close(release)
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, topID)
		if record.State.Outcome.Status != "aborted" || strings.Join(log, ",") != "x,b,a" {
			t.Fatal(record, log)
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[xID].Owner != 0 || state.Conversations[state.Tasks[xID].Conversation].Owner != bID {
			t.Fatal("conversation ownership bypassed", state.Tasks[xID], err)
		}
	})
}
