package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// Consolidated from harness_hook_reads_v150_test.go.

// Kinds are asserted exactly as stored; no native role-to-kind normalisation.
func v150HistoryKinds(t *testing.T, entries []Entry) []string {
	t.Helper()
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Kind)
	}
	return out
}

// Complete pinned harness-hook-reads.test.ts:18 scenario: the hook sees only the
// committed input before the answer is stored, including single/typed reads.
func TestV150HookReadsCommittedRawHistory(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	userToken, err := DefineEntry(userEntryKind)
	if err != nil {
		t.Fatal(err)
	}
	wrongToken, err := DefineEntry("pi.generation")
	if err != nil {
		t.Fatal(err)
	}
	var seen [][]string
	var captured *HookAPI
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "reader", Hooks: GenerationHooks{OnYield: func(ctx context.Context, _ MessageReceipt, api *HookAPI) (*Input, error) {
		captured = api
		page, err := api.Entries(ctx, EntryQuery{Order: Ascending}, 10, nil)
		if err != nil {
			return nil, err
		}
		seen = append(seen, v150HistoryKinds(t, page.Items))
		if len(page.Items) != 1 {
			t.Errorf("pre-answer entries=%v", page.Items)
			return nil, nil
		}
		entry, found, err := api.TypedEntry(ctx, userToken, page.Items[0].ID)
		if err != nil || !found || !reflect.DeepEqual(v150HistoryKinds(t, []Entry{entry}), []string{"pi.user"}) {
			t.Errorf("user read found=%v err=%v", found, err)
		}
		if wrong, found, err := api.TypedEntry(ctx, wrongToken, page.Items[0].ID); err != nil || found || wrong.ID != 0 {
			t.Errorf("wrong token returned=%+v found=%v err=%v", wrong, found, err)
		}
		return nil, nil
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
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, submission); result.Record.Status != "done" {
		t.Fatal(result)
	}
	if err := conversation.WaitForIdle(bg); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, [][]string{{"pi.user"}}) {
		t.Fatalf("hook history=%v", seen)
	}
	if captured == nil {
		t.Fatal("hook not invoked")
	}
	if _, err := captured.Entries(bg, EntryQuery{}, 10, nil); !errors.Is(err, ErrSealed) {
		t.Fatalf("escaped hook history=%v", err)
	}
}

// Complete pinned harness-hook-reads.test.ts:51 scenario: two one-entry pages,
// continuation termination and an inclusive one-ID range from an actual task.
func TestV150TaskPagesBoundedRawHistory(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var pages [][]ID
	var captured *TaskRuntime
	reader := taskDefinition(t, "test.reader", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		captured = r
		first, err := r.Entries(ctx, EntryQuery{Order: Ascending}, 1, nil)
		if err != nil {
			return err
		}
		if len(first.Items) != 1 || first.Next == nil {
			t.Errorf("first page=%+v", first)
			return nil
		}
		second, err := r.Entries(ctx, EntryQuery{Order: Ascending}, 1, first.Next)
		if err != nil {
			return err
		}
		if len(second.Items) != 1 || second.Next != nil {
			t.Errorf("second page=%+v", second)
			return nil
		}
		bounded, err := r.Entries(ctx, EntryQuery{MinEntryID: second.Items[0].ID, MaxEntryID: second.Items[0].ID}, 10, nil)
		if err != nil {
			return err
		}
		for _, page := range []Page[Entry]{first, second, bounded} {
			ids := []ID{}
			for _, entry := range page.Items {
				ids = append(ids, entry.ID)
			}
			pages = append(pages, ids)
		}
		// Native caller-control and ownership checks supplement the source scenario.
		if _, err := r.Entries(ctx, EntryQuery{Conversation: ID(MaxID)}, 1, nil); err == nil {
			t.Error("foreign conversation accepted")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := r.Entries(canceled, EntryQuery{}, 1, nil); !errors.Is(err, context.Canceled) {
			t.Errorf("canceled history=%v", err)
		}
		first.Items[0].Value["mutated"] = true
		again, _, err := r.Entry(ctx, first.Items[0].ID)
		if err != nil || again.Value["mutated"] != nil {
			t.Errorf("history aliases stored data: %v", err)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	registry := NewRegistry()
	if _, err := registry.RegisterTask(reader); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- terminal("done")
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, submission)
	id := createPublicTask(t, h, reader, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if result := waitPublicTask(t, h, id); result.State.Outcome.Status != "completed" {
		t.Fatal(result)
	}
	all, err := conversation.Entries(bg, EntryQuery{Order: Ascending}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v150HistoryKinds(t, all.Items), []string{"pi.user", "pi.assistant"}) {
		t.Fatal("raw history kinds", v150HistoryKinds(t, all.Items))
	}
	if len(all.Items) != 2 || !reflect.DeepEqual(pages, [][]ID{{all.Items[0].ID}, {all.Items[1].ID}, {all.Items[1].ID}}) {
		t.Fatalf("pages=%v all=%+v", pages, all)
	}
	if _, err := captured.Entries(bg, EntryQuery{}, 1, nil); !errors.Is(err, ErrSealed) {
		t.Fatalf("ended task history=%v", err)
	}
}

// Consolidated from harness_after_run_history_v150_test.go.

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
			CompactionHooks: CompactionHooks{BeforeCompact: func(context.Context, BeforeCompactInput, *HookAPI) (*CompactionDecision, error) {
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
				if err := decodeEntryMessage(first, h.session.limits, &user); err != nil || user.Role != goai.RoleUser || user.ContentText == nil || *user.ContentText != "go" || user.Content != nil {
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
		compaction, err := c.Compact(bg, nil)
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
