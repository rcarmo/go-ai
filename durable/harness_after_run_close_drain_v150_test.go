package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Pinned harness-after-run.test.ts:429 orderly-close half. Source waits for an
// actual task attempt, closes, reopens and checks a rerun without re-asking the
// hook. The old pending fixture incorrectly expected the durable abort handler
// and raced task reservation against actual phase entry. Those translation
// errors are corrected; the original runtime already passes this scenario.
func TestV150AfterRunCloseInDrainResumesInterruptedAttempt(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var executions, commits, aborts, asks atomic.Int64
		gate := make(chan struct{})
		def, err := DefineTask(TaskDefinitionOptions{Kind: "task.close-drain", Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
			Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				executions.Add(1)
				select {
				case <-gate:
				case <-ctx.Done():
				}
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					commits.Add(1)
					id, err := tx.MintID()
					if err != nil {
						return nil, err
					}
					if err := tx.AppendEntry(Entry{ID: id, Conversation: r.ConversationID(), Kind: "test.tests", Value: JSON{"label": "drain"}, ByTask: r.TaskID()}); err != nil {
						return nil, err
					}
					return taskDone(JSON{"label": "drain"}), nil
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
			return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": "drain"}}}}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
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
		waitSubmission(t, sub)
		childID := ID(0)
		deadline := time.Now().Add(3 * time.Second)
		for childID == 0 {
			live := mainAfterRunLive(t, h, c.ID())
			if a, ok := generationObject(live["run"])["afterRun"].(map[string]any); ok {
				if list, islist := a["tasks"].([]any); islist && len(list) == 1 {
					childID = submissionEntryID(list[0])
				}
			}
			if childID == 0 {
				if time.Now().After(deadline) {
					t.Fatal("the hook's drain task was never committed", asks.Load())
				}
				time.Sleep(time.Millisecond)
			}
		}
		runningDeadline := time.Now().Add(3 * time.Second)
		for {
			child, ok, err := h.Task(bg, childID)
			if err != nil || !ok {
				t.Fatal("drain task missing", ok, err)
			}
			// Source awaits the actual phase attempt, not its earlier reservation.
			if child.State.Status == "running" && executions.Load() == 1 {
				break
			}
			if time.Now().After(runningDeadline) {
				t.Fatal("drain task never started", child.State.Status, child.State.Outcome)
			}
			time.Sleep(time.Millisecond)
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
		if executions.Load() != 2 || commits.Load() != 1 {
			t.Error("interrupted drain attempt was not re-executed once", executions.Load(), commits.Load())
		}
		// The source also requires exactly one stored test entry across reopen.
		page, err := c2.ScanEntries(ctx, EntryQuery{Order: Ascending}, 100, "")
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
		if live := mainAfterRunLive(t, h2, c.ID()); len(live) != 0 {
			t.Error("run/live after the resumed drain", live)
		}
		if err := h2.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
