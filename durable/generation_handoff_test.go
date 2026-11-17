package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationToolHandoffCommittedReopenNoToolReplayAndTerminalSubmissionTask(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		var tools, calls atomic.Int64
		registration := wrapRegistration("handoff")
		registration.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
			tools.Add(1)
			return ToolResult{Content: "spent tool"}, nil
		}
		if err := registry.Register(registration); err != nil {
			t.Fatal(err)
		}
		entered := make(chan struct{})
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			switch calls.Add(1) {
			case 1:
				ch <- toolAnswer("handoff-call", "handoff", JSON{})
				close(ch)
			case 2:
				close(entered)
				go func() { <-ctx.Done(); close(ch) }()
			default:
				results, inputs := 0, 0
				for _, m := range input.Messages {
					if m.Role == goai.RoleToolResult && m.ToolCallID == "handoff-call" {
						results++
					}
					if m.Role == goai.RoleUser {
						inputs++
					}
				}
				if results != 1 || inputs != 1 {
					t.Errorf("handoff replay duplicated transcript: results=%d inputs=%d", results, inputs)
				}
				ch <- terminal("successor answer")
				close(ch)
			}
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "handoff request", RequestID: "handoff-once"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		view, err := conversation.View(bg)
		if err != nil {
			t.Fatal(err)
		}
		live, _ := view.Docs.Get("pi.live")
		run, _ := live.Get("run")
		taskNode, _ := run.Get("taskId")
		successor := submissionEntryID(taskNode.Copy())
		pending, found, err := h.Task(bg, successor)
		if err != nil || !found || pending.Owner != 0 || pending.Kind != "pi.generation" || pending.State.Status != "running" || taskCheckpointMap(pending.State.Checkpoint)["phase"] != "request" {
			t.Fatal("handoff successor", pending, err)
		}
		first := ID(0)
		for id, task := range state.Tasks {
			if task.Kind != "pi.generation" || id == successor {
				continue
			}
			record, found, err := h.Task(bg, id)
			if err != nil || !found || record.State.Status != "terminal" || record.State.Outcome == nil || record.State.Outcome.Status != "completed" {
				t.Fatal("handoff predecessor", record, err)
			}
			first = id
			entryID := submissionEntryID(generationObject(record.State.Outcome.Result.Value)["entryId"])
			entry, found, err := h.Entry(bg, entryID)
			if err != nil || !found || entry.Kind != "pi.assistant" {
				t.Fatal("predecessor receipt", entry, err)
			}
		}
		status, err := sub.Status(bg)
		if err != nil || status.Status != "placed" || first == 0 {
			t.Fatal("handoff input prematurely settled", status, first, err)
		}
		firstEntry := status.Entry
		firstNode, _ := run.Get("first")
		inputsNode, _ := run.Get("inputs")
		if firstEntry == 0 || submissionEntryID(firstNode.Copy()) != firstEntry || !equalJSONValue(inputsNode.Copy(), []any{int(sub.ID())}) {
			t.Fatal("handoff lost inputs", run.Copy(), status)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		h = openHarness(t, store, options)
		if tools.Load() != 1 || calls.Load() != 2 {
			t.Fatal("Open dispatched effects", tools.Load(), calls.Load())
		}
		handle, err := h.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || result.Task.ID != successor || result.Message == nil || result.Message.Content[0].Text != "successor answer" || tools.Load() != 1 || calls.Load() != 3 {
			t.Fatal("successor settlement/replay", result, tools.Load(), calls.Load())
		}
		// Public dedup must return the original input and final successor result.
		conversation, err = h.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := conversation.Submit(bg, Input{Content: "ignored", RequestID: "handoff-once"})
		if err != nil || repeated.ID() != sub.ID() {
			t.Fatal(repeated, err)
		}
		if settled := waitSubmission(t, repeated); settled.Task.ID != successor || calls.Load() != 3 {
			t.Fatal("dedup replayed or picked ancestor", settled)
		}
		state, err = h.Snapshot(bg)
		if err != nil || len(state.Tasks) != 3 {
			t.Fatal("handoff task replay duplicated", len(state.Tasks), err)
		}
		// Source handOver changes live.run.taskId; it does not persist a
		// successor/submission pointer in the terminal predecessor checkpoint.
		previous, found, err := h.Task(bg, first)
		if err != nil || !found || previous.State.Checkpoint != nil {
			t.Fatal("terminal predecessor checkpoint", previous, err)
		}
	})
}
