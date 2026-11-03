package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Actual f9a26365 harness-after-run.test.ts694, narrowed to what the current
// native public hook API can express. The source reads the run's whole history
// with api.entries(minEntryId..maxEntryId) and asserts the exact kind sequence;
// the native HookAPI exposes no range scan, so that ordering is not asserted
// here. What is asserted is the same distinction the source makes: the active
// context lost the run's input to the mid-run summary, while the run's committed
// history keeps it.
//
// Known-failing oracle. The fixture fails before reaching the history checks
// because the AfterRun hook is not invoked at all for a run whose answer follows
// a model tool round, while the pinned contract evaluates the selected AfterRun
// hooks on the answering path. The failure is not a selection artifact: it also
// reproduces with the extension selected explicitly on the agent, with no gate
// and no compaction, so the tool-round continuation itself skips the hook.
// Assertions are retained unchanged; no default skip or relaxed expectation.
func TestMainAfterRunHookHistoryKeepsRunStartHiddenByMidRunCompaction(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var h *Harness
		started := make(chan struct{})
		release := make(chan struct{})
		summary := "earlier work"
		var requests atomic.Int64
		var hook struct {
			called      bool
			summary     RunSummary
			firstRead   bool
			headID      ID
			headKind    string
			headInRun   bool
			firstInView bool
		}
		registry := NewRegistry()
		err := registry.Install(&Extension{
			Name: "after-run-compaction-history",
			Tools: []ToolRegistration{{Definition: goai.Tool{Name: "edit", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "test.edit", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return ToolResult{}, ctx.Err()
				}
				return ToolResult{Content: "edited"}, nil
			}}},
			CompactionHooks: CompactionHooks{BeforeCompact: func(context.Context, CompactionInput, *HookAPI) (*CompactionDecision, error) {
				return &CompactionDecision{Summary: &summary}, nil
			}},
			Hooks: GenerationHooks{AfterRun: func(ctx context.Context, run RunSummary, api *HookAPI) (*AfterRunResult, error) {
				hook.called = true
				hook.summary = run
				first, ok, err := api.Entry(ctx, run.First)
				if err != nil || !ok {
					t.Errorf("hook cannot read the run start hidden from the active context: ok=%v err=%v", ok, err)
					return nil, nil
				}
				hook.firstRead = true
				var user MessageReceipt
				if err := fromObject(first.Value, &user, h.session.limits); err != nil || user.Role != goai.RoleUser || goai.ContentText(user.Content) != "go" {
					t.Errorf("committed run start must remain the user input: kind=%q receipt=%+v err=%v", first.Kind, user, err)
				}
				view, err := api.ContextView(ctx, api.ConversationID(), run.Last)
				if err != nil {
					t.Errorf("hook cannot read the active context view: err=%v", err)
					return nil, nil
				}
				if view.Head == nil {
					t.Error("active context must start at the mid-run summary")
					return nil, nil
				}
				hook.headID, hook.headKind = view.Head.ID, view.Head.Kind
				hook.headInRun = view.Head.ID > run.First && view.Head.ID < run.Last
				for _, entry := range view.Entries {
					if entry.ID == run.First {
						hook.firstInView = true
					}
				}
				return nil, nil
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			switch requests.Add(1) {
			case 1:
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "edit-call", Name: "edit", Arguments: map[string]any{}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			case 2:
				// The summary came from the hook, so no model request is spent on it.
				for _, message := range input.Messages {
					if message.Role == goai.RoleUser && goai.ContentText(message.Content) == "go" {
						t.Error("active context must not still carry the summarized run start")
					}
				}
				ch <- terminal("done")
			default:
				t.Error("unexpected provider request after the answer")
				ch <- terminal("unexpected")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h = openHarness(t, b.store, options)
		c, err := h.Root(bg, AgentChange{Model: ref, SystemPrompt: "system", Settings: RequestSettings{Compaction: CompactionPolicy{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: 1, BackgroundTokens: 0}}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("tool did not start")
		}
		// The summary is placed in the middle of the run, before its answer.
		compaction, err := c.Compact(bg, CompactionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		task, err := h.WaitForTask(ctx, compaction)
		if err != nil {
			t.Fatal(err)
		}
		if task.State.Status != "terminal" || task.State.Outcome == nil || task.State.Outcome.Status != "completed" || task.State.Outcome.Result == nil {
			t.Fatal("compaction did not complete", task)
		}
		releaseTaskGate(release)
		settled := waitSubmission(t, sub)
		if settled.Record.Status != "done" || settled.Record.Answer == 0 {
			t.Fatal("run did not settle", settled)
		}
		if !hook.called {
			t.Fatal("afterRun hook was not invoked for the tool-round run", hook.summary)
		}
		if !hook.firstRead {
			t.Fatal("hook did not read the committed run start", hook.summary)
		}
		if hook.headKind != "pi.compaction" || !hook.headInRun {
			t.Error("mid-run summary is not inside the run's committed history", hook.headID, hook.headKind, settled)
		}
		if hook.firstInView {
			t.Error("run start is still in the active context after compaction", hook)
		}
		if requests.Load() != 2 {
			t.Error("summary or answer request count", requests.Load())
		}
		if live := mainAfterRunLive(t, h, c.ID()); len(live) != 0 {
			t.Error("run did not clear after the hook", live)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
