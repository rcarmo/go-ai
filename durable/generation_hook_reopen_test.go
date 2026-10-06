package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationHookHeldHandoffReopenDoesNotReplayHooksOrRequests(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{})
		var childRuns, hookCalls, requests atomic.Int64
		var owner, childID ID
		registry := NewRegistry()
		child := taskDefinition(t, "task.hook.reopen", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if childRuns.Add(1) == 1 {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("resumed"), nil })
		})
		if _, err := registry.RegisterTask(child); err != nil {
			t.Fatal(err)
		}
		tool := wrapRegistration("noop")
		tool.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "done"}, nil }
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
		if err := registry.Install(&Extension{Name: "hook.reopen", Hooks: GenerationHooks{AfterToolEntries: func(ctx context.Context, assistant ID, results []ID, api *HookAPI) error {
			hookCalls.Add(1)
			owner = api.TaskID()
			if len(results) != 1 || assistant == 0 {
				t.Error("missing hook entry identities", assistant, results)
			}
			if err := api.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				id, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
				childID = id
				return nil, err
			}); err != nil {
				return err
			}
			awaitTaskSignal(t, entered)
			return nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				ch <- toolAnswer("call", "noop", JSON{})
			} else {
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		sub, err := root(t, h, ref).Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if before.Tasks[owner].Status != "completing" || before.Tasks[childID].Status != "running" {
			t.Fatal("handoff not held", before.Tasks[owner], before.Tasks[childID])
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		paused, err := reopened.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if paused.Tasks[owner].Status != "completing" || requests.Load() != 2 || hookCalls.Load() != 1 || childRuns.Load() != 1 {
			t.Fatal("Open replayed held handoff", requests.Load(), hookCalls.Load(), childRuns.Load())
		}
		// Terminal submission lookup stays passive; waiting the held task resumes
		// only its surviving child and performs deterministic final retirement.
		settled, err := (&SubmissionHandle{h: reopened, id: sub.ID()}).Wait(bg)
		if err != nil || settled.Submission.Status != "done" {
			t.Fatal(settled, err)
		}
		waitPublicTask(t, reopened, owner)
		final, err := reopened.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if final.Tasks[owner].Status != "done" || final.Tasks[childID].Status != "done" || requests.Load() != 2 || hookCalls.Load() != 1 || childRuns.Load() != 2 {
			t.Fatal("held handoff replayed or failed to retire", requests.Load(), hookCalls.Load(), childRuns.Load())
		}
		hold := final.Tasks[owner].Execution.Builtin.Hold
		if hold.Action != "generation-handoff" || hold.Stage != "final" {
			t.Fatal("handoff final disposition", hold)
		}
		if _, err := h.Conversation(bg, 1); !errors.Is(err, ErrClosed) {
			t.Fatal("closed original harness retained authority", err)
		}
	})
}
