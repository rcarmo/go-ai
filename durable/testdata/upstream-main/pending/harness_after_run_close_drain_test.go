package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Known-failing oracle for the unverified half of pinned source429
// (harness-after-run.test.ts429): "An orderly close in `drain` resumes the same
// way." The source shape is kept: the drain task records its attempt, waits for
// the gate or its abort, then attempts its commit, and the abort handler records
// the aborted outcome. The source expects the interrupted attempt to leave no
// decided outcome, so the reopened harness re-executes the phase (two attempts,
// one commit) and the hook is never re-asked once its tasks exist.
//
// Observed native behaviour at revision e7a17b0 (failing baseline, one run,
// 0.086s): Close does not cancel the running drain task — no abort handler runs
// (aborts=0), the phase continues past cancellation and its commit lands while the
// harness closing flag is true, so the task completes at close with one attempt and
// the reopen re-execution the source asserts is never reached. When the handler
// instead surfaces cancellation as an error, native records a decided `faulted`
// outcome rather than leaving the interrupted attempt pending. The source's entry
// count (`kindOf(..., "test.tests")` length 1) is not ported: the native public API
// exposes no entry range scan. No assertion here is weakened, and no runtime change
// was made pending reconciliation of the close-fence contracts.
func TestMainAfterRunCloseInDrainResumesInterruptedAttempt(t *testing.T) {
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
			if child.State.Status == "running" {
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
		if aborts.Load() != 1 {
			t.Error("close did not run the drain task's abort handler", aborts.Load())
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
