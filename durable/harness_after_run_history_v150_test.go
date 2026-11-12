package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Complete f9a26365 harness-after-run.test.ts:694 history assertion. Native
// stored entry kinds are compared literally, without role-to-kind normalisation. The old pending fixture omitted source waitForIdle
// and falsely diagnosed a missing callback; after restoring that wait, unchanged
// generation runtime passes. Entries now supplies the original bounded scan.
func TestV150AfterRunHookHistoryKeepsRunStartHiddenByMidRunCompaction(t *testing.T) {
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
			kinds       []string
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
				page, err := api.Entries(ctx, EntryQuery{MinEntryID: run.First, MaxEntryID: run.Last, Order: Ascending}, 100, nil)
				if err != nil {
					t.Errorf("run history scan: %v", err)
					return nil, nil
				}
				for _, kind := range v150HistoryKinds(t, page.Items) {
					if kind != "pi.system" {
						hook.kinds = append(hook.kinds, kind)
					}
				}
				first, ok, err := api.Entry(ctx, run.First)
				if err != nil || !ok {
					t.Errorf("hook cannot read the run start hidden from the active context: ok=%v err=%v", ok, err)
					return nil, nil
				}
				hook.firstRead = true
				var user MessageReceipt
				if err := decodeEntryMessage(first, h.session.limits, &user); err != nil || user.Role != goai.RoleUser || goai.ContentText(user.Content) != "go" {
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
		cleanupTaskGates(t, release)
		c, err := h.Root(bg, AgentChange{Model: ref, SystemPrompt: "system", Settings: RequestSettings{Compaction: CompactionPolicy{Enabled: false, ReserveTokens: 1000, KeepRecentTokens: 1, BackgroundTokens: 0}}})
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
		// Source waits for the run, not merely the already-settled input.
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(hook.kinds, []string{"pi.user", "pi.assistant", "pi.tool-result", "pi.compaction", "pi.assistant"}) {
			t.Errorf("raw run history kinds: %v", hook.kinds)
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
