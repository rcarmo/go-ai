package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestToolSchedulerFaultHoldsEndAndFailureUntilOwnedHostDrains(t *testing.T) {
	registry := NewRegistry()
	childEntered, childRelease, toolEntered, toolRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, childRelease, toolRelease)
	var childID atomic.Uint64
	child := taskDefinition(t, "task.fault-owned", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
		close(childEntered)
		<-childRelease
		return ctx.Err()
	})
	// After close/reopen the child has a fresh cooperative abort handler whose
	// actual return remains gated after its terminal state is committed.
	child.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "owner_fault"}}, nil
		}); err != nil {
			return err
		}
		<-childRelease
		return nil
	}
	if _, err := registry.RegisterTask(child); err != nil {
		t.Fatal(err)
	}
	reg := wrapRegistration("work")
	reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
		id, err := api.CreateTask(ctx, child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: api.TaskID()}})
		if err != nil {
			return ToolResult{}, err
		}
		childID.Store(uint64(id))
		<-childEntered
		close(toolEntered)
		<-toolRelease
		<-ctx.Done()
		return ToolResult{}, ctx.Err()
	}
	if err := registry.Register(reg); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if calls.Add(1) == 1 {
			ch <- toolAnswer("call", "work", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, dir := newJournal(t)
	h := openHarness(t, store, options)
	sub, err := root(t, h, ref).Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	<-toolEntered
	closed := make(chan error, 1)
	go func() { closed <- h.Close(bg) }()
	<-h.life.Done()
	close(toolRelease)
	close(childRelease)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	// Use new gates for the reopened child abort, with no executor replay.
	abortEntered, abortRelease := make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, abortRelease)
	replacement := taskDefinition(t, "task.fault-owned", func(context.Context, TaskRecord, *TaskRuntime) error {
		t.Error("ordinary child restarted below decided fault")
		return nil
	})
	replacement.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "owner_fault"}}, nil
		}); err != nil {
			return err
		}
		close(abortEntered)
		<-abortRelease
		return nil
	}
	if _, err := registry.RegisterTask(replacement); err != nil {
		t.Fatal(err)
	}
	store, err = OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h = openHarness(t, store, options)
	snapshot, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var toolID ID
	for _, task := range snapshot.Tasks {
		if task.Kind == "pi.tool" {
			toolID = task.ID
		}
	}
	stream, err := h.WatchEvents(bg, snapshot.Tasks[toolID].Conversation)
	if err != nil {
		t.Fatal(err)
	}
	var ends, failures atomic.Int64
	doneEvent := make(chan struct{}, 1)
	if err := stream.Start(func(_ context.Context, events []AgentEvent) error {
		for _, event := range events {
			if event.Task == toolID && event.Type == "tool_execution_end" {
				if event.Message != nil {
					t.Error("fault end invented receipt")
				}
				ends.Add(1)
				select {
				case doneEvent <- struct{}{}:
				default:
				}
			}
			if event.Task == toolID && event.Type == "task_failed" {
				failures.Add(1)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err = h.session.taskCommit(bg, func(tx *Tx) error {
		return h.scheduler.builtinDecision(tx, tx.state.Tasks[toolID], TaskOutcome{Status: "faulted", Error: &TaskOutcomeError{Message: "held_fault"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, abortEntered)
	snapshot, err = h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Tasks[toolID].Status != "completing" || !terminalStatus(snapshot.Tasks[ID(childID.Load())].Status) || ends.Load() != 0 || failures.Load() != 0 || calls.Load() != 1 {
		t.Fatal("fault ended before actual owned host return", snapshot.Tasks[toolID], ends.Load(), failures.Load(), calls.Load())
	}
	close(abortRelease)
	resumed, err := h.Submission(bg, sub.ID())
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, resumed)
	awaitTaskSignal(t, doneEvent)
	if ends.Load() != 1 || failures.Load() != 1 {
		t.Fatal("terminal fault events", ends.Load(), failures.Load())
	}
	stream.Stop()
}
