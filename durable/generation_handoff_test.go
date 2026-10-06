package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationToolHandoffCommittedReopenNoToolReplayAndTerminalSubmissionTask(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var tools, calls atomic.Int64
		registration := wrapRegistration("handoff")
		registration.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
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
		var first, successor ID
		for _, task := range state.Tasks {
			if task.Kind != "pi.generation" {
				continue
			}
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
				t.Fatal(err)
			}
			if cp.Successor != 0 {
				first, successor = task.ID, cp.Successor
			}
		}
		if first == 0 || successor == 0 || state.Tasks[first].Status != "done" || state.Tasks[successor].Owner != 0 || terminalStatus(state.Submissions[sub.ID()].Status) {
			t.Fatal("handoff not committed atomically", first, successor, state)
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
		// Corrupt a detached final handoff envelope: neither missing successor nor
		// another submission is valid. Candidate validation must reject both.
		bad := state
		bad.Tasks = make(map[ID]Task, len(state.Tasks))
		for id, task := range state.Tasks {
			bad.Tasks[id] = task
		}
		delete(bad.Tasks, successor)
		if err := validateTaskReferences(bad, bad.Tasks[first], h.session.limits); err == nil {
			t.Fatal("missing successor accepted")
		}
		var rejected *StorageRejected
		if err := validateTaskReferences(bad, bad.Tasks[first], h.session.limits); !errors.As(err, &rejected) {
			t.Fatal("handoff failure not rejected", err)
		}
		bad.Tasks[successor], err = copyTask(state.Tasks[successor], h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		wrong := bad.Tasks[successor]
		wrong.Checkpoint["submission"] = ID(MaxID)
		bad.Tasks[successor] = wrong
		if err := validateTaskReferences(bad, bad.Tasks[first], h.session.limits); !errors.As(err, &rejected) {
			t.Fatal("foreign handoff submission accepted", err)
		}
	})
}
