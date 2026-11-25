package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-after-run.test.ts:459–475. Orderly close cancels the real child
// invocation without calling its durable abort handler. Opening the same
// database reruns that child without asking its parent's hook again.
func TestV150AfterRunCloseInDrainResumesInterruptedAttempt(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var executions, commits, aborts, asks, requests atomic.Int64
		gate := make(chan struct{})
		entered := make(chan TaskRecord, 2)
		def, err := DefineTask(TaskDefinitionOptions{Kind: "task.close-drain", Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
			Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				if executions.Add(1) <= 2 {
					entered <- task
				}
				label := generationObject(task.Input.Value)["label"]
				select {
				case <-gate:
				case <-ctx.Done():
				}
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					commits.Add(1)
					id, err := tx.mintID()
					if err != nil {
						return nil, err
					}
					if err := tx.appendEntryRecord(Entry{ID: id, Conversation: r.ConversationID(), Kind: "test.tests", Value: JSON{"label": label}, ByTask: r.TaskID()}); err != nil {
						return nil, err
					}
					return taskDone(JSON{"label": label}), nil
				})
			}},
			Abort: func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				aborts.Add(1)
				return r.Commit(ctx, func(_ *Tx, _ TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
				})
			}})
		if err != nil {
			t.Fatal(err)
		}
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "close-drain", Tasks: []*TaskDefinition{def}, Hooks: GenerationHooks{AfterRun: func(_ context.Context, run RunSummary, _ *HookAPI) (*AfterRunResult, error) {
			asks.Add(1)
			return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": "run 1"}}}}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			requests.Add(1)
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, gate)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		answer := waitSubmission(t, sub).Record
		first := watchReceive(t, entered)
		childID := first.ID
		if child, ok, err := h.Task(bg, childID); err != nil || !ok || child.State.Status != "running" || child.Owner != readOnlyHarnessRunID(t, c) || child.Restarts != 0 || generationObject(child.Input.Value)["label"] != "run 1" {
			t.Fatal("original drain attempt did not start as the owned child run 1", child, ok, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal("orderly close during drain", err)
		}
		// Source orderly close cancels an invocation; it does not persist an
		// abort mark or run the task's durable abort handler.
		if aborts.Load() != 0 {
			t.Error("orderly close invoked the durable abort handler", aborts.Load())
		}
		close(gate)
		h2 := openHarness(t, b.reopen(), options)
		c2, err := h2.Conversation(bg, c.ID())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c2.WaitForIdle(ctx); err != nil {
			t.Fatal("reopen did not resume the interrupted drain", err)
		}
		second := watchReceive(t, entered)
		if second.ID != childID || second.Owner != first.Owner || generationObject(second.Input.Value)["label"] != "run 1" {
			t.Fatal("orderly close did not resume the same owned child run 1", second)
		}
		if executions.Load() != 2 || commits.Load() != 1 {
			t.Error("interrupted drain attempt was not re-executed once", executions.Load(), commits.Load())
		}
		// The source also requires exactly one stored test entry across reopen.
		page, err := c2.Entries(ctx, EntryQuery{Order: Ascending}, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range page.Items {
			if entry.Kind == "test.tests" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("test entry count=%d", count)
		}
		if asks.Load() != 1 {
			t.Error("hook was re-asked after its tasks existed", asks.Load())
		}
		resumed, ok, err := h2.Task(bg, childID)
		if err != nil || !ok {
			t.Fatal("resumed task missing", ok, err)
		}
		if resumed.State.Outcome == nil || resumed.State.Outcome.Status != "completed" {
			t.Error("resumed drain task not completed", resumed.State.Status, resumed.State.Outcome)
		}
		parent, found, err := h2.Task(ctx, first.Owner)
		outcome := parent.State.Outcome
		if err != nil || !found || parent.State.Status != "terminal" || outcome == nil || outcome.Status != "completed" || outcome.Result == nil || !equalJSONValue(outcome.Result.Value, JSON{"entryId": float64(answer.Answer)}) || requests.Load() != 1 {
			t.Fatal("orderly close changed the original answer or repeated the provider request", parent, found, err, requests.Load())
		}
		if live := mainAfterRunLive(t, h2, c.ID()); len(live) != 0 {
			t.Error("run/live after the resumed drain", live)
		}
		if err := h2.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
