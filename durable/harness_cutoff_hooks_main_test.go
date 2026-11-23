package durable

// Official v1.5.0 f9a26365 harness-tool-hooks.test.ts584, plus labelled
// source-derived normalization/error controls for tool.ts afterHarnessResult.
import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type mainCutoffHooks struct {
	options        Options
	ref            ModelRef
	started        chan struct{}
	runs, requests atomic.Int64
	mu             sync.Mutex
	errors         []error
	infos          []ReportInfo
}

func mainCutoffHooksSetup(t *testing.T, after func(context.Context, goai.ToolCall, ToolResult, *HookAPI) (*ToolResult, error), limits *ToolOutputLimits) *mainCutoffHooks {
	t.Helper()
	f := &mainCutoffHooks{started: make(chan struct{}, 4)}
	f.ref, f.options = setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if f.requests.Add(1) == 1 {
			ch <- toolAnswer("c1", "work", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	limit := uint64(1)
	zero := int64(0)
	window := float64(0)
	f.options.Settings = &HarnessSettings{RestartLimit: &limit, Progress: &ProgressSettings{OutputIntervalMs: &zero, PersistWithinMs: &window}}
	f.options.Registry = NewRegistry()
	err := f.options.Registry.Install(&Extension{Name: "cutoff-result", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "work", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "cutoff.result", Version: 1, OutputLimits: limits, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		n := f.runs.Add(1)
		if err := api.Output("work " + strings.Repeat("o", 40) + "\n"); err != nil {
			return ToolResult{}, err
		}
		if err := api.Details(JSON{"run": n}); err != nil {
			return ToolResult{}, err
		}
		f.started <- struct{}{}
		<-ctx.Done()
		return ToolResult{}, ctx.Err()
	}}}, ToolHooks: ToolHooks{AfterTool: after}})
	if err != nil {
		t.Fatal(err)
	}
	f.options.OnReportWithInfo = func(_ context.Context, err error, info ReportInfo) {
		if info.Type == "extension_error" {
			f.mu.Lock()
			f.errors = append(f.errors, err)
			f.infos = append(f.infos, info)
			f.mu.Unlock()
		}
	}
	return f
}
func mainCutoffHooksStart(t *testing.T, f *mainCutoffHooks, store Storage) (*Harness, *ConversationHandle, ID, ID) {
	t.Helper()
	h := openHarness(t, store, f.options)
	c := root(t, h, f.ref)
	sub, err := c.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, f.started)
	// Native details and this dependent read force the actual slot into storage.
	if _, err := c.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := builtin(tx, c.ID(), "pi.live")
		if err != nil {
			return nil, err
		}
		_, err = doc.Get()
		return nil, err
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := testStorageProjection(store).Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var id ID
	for _, task := range snap.Tasks {
		if task.Kind == "pi.tool" {
			id = task.ID
		}
	}
	if id == 0 {
		t.Fatal("stored tool task missing")
	}
	return h, c, sub.ID(), id
}
func mainCutoffHooksOpen(t *testing.T, f *mainCutoffHooks, store Storage, cid ID) (*Harness, *ConversationHandle) {
	t.Helper()
	h := openHarness(t, store, f.options)
	c, err := h.Conversation(bg, cid)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	return h, c
}
func mainCutoffHooksReceipt(t *testing.T, h *Harness, id ID) MessageReceipt {
	t.Helper()
	snap, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range snap.Entries {
		if e.ByTask == id && messageEntryValue(e)["role"] == "toolResult" {
			var receipt MessageReceipt
			if err := decodeEntryMessage(e, h.session.limits, &receipt); err != nil {
				t.Fatal(err)
			}
			return receipt
		}
	}
	t.Fatal("tool receipt missing")
	return MessageReceipt{}
}
func mainCutoffCodes(result ToolResult) []string {
	var out []string
	for _, d := range result.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}
func mainCutoffHooksDone(t *testing.T, h *Harness, c *ConversationHandle, submission, tool ID) TaskRecord {
	t.Helper()
	handle, err := h.Submission(bg, submission)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitSubmission(t, handle); got.Submission.Status != "done" {
		t.Error(got)
	}
	mainAfterRecoveryIdle(t, c)
	task := mainCutoffTask(t, h, tool)
	if task.State.Outcome == nil || task.State.Outcome.Status != "failed" || task.State.Outcome.Error == nil || task.State.Outcome.Error.Detail == nil || !equalJSONValue(task.State.Outcome.Error.Detail.Value, JSON{"reason": "restart_limit"}) {
		t.Error("cutoff outcome", task)
	}
	return task
}
func TestMainCutoffHooksAtLimitNotPastLimit(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered := make(chan struct{}, 4)
		var mu sync.Mutex
		var seen [][]string
		f := mainCutoffHooksSetup(t, func(ctx context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			mu.Lock()
			seen = append(seen, mainCutoffCodes(result))
			mu.Unlock()
			entered <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}, nil)
		h, c, sub, tool := mainCutoffHooksStart(t, f, b.store)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, c = mainCutoffHooksOpen(t, f, store, c.ID())
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Error("cutoff hook never entered at restartLimit")
		}
		mu.Lock()
		first := append([][]string(nil), seen...)
		mu.Unlock()
		if !reflect.DeepEqual(first, [][]string{{"restart_limit"}}) {
			t.Error("at-limit hook input", first)
		}
		store = mainAfterRecoveryCrash(t, h, store)
		h, c = mainCutoffHooksOpen(t, f, store, c.ID())
		mainCutoffHooksDone(t, h, c, sub, tool)
		mu.Lock()
		count := len(seen)
		mu.Unlock()
		if count != 1 || f.runs.Load() != 1 {
			t.Error("past-limit hook/tool repeated", count, f.runs.Load())
		}
		receipt := mainCutoffHooksReceipt(t, h, tool)
		var codes []string
		for _, d := range receipt.Diagnostics {
			codes = append(codes, d.Code)
		}
		if !reflect.DeepEqual(codes, []string{"restart_limit"}) {
			t.Error(codes)
		}
	})
}

// Source-derived: tool.ts758 applies the hook replacement, current output limits,
// drops structuredOutput and records usage; failed ending remains independent.
func TestMainCutoffHooksNormalizeReplacement(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		f := mainCutoffHooksSetup(t, func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			calls.Add(1)
			if !result.IsError || !reflect.DeepEqual(mainCutoffCodes(result), []string{"restart_limit"}) {
				t.Error("hook input", result)
			}
			return &ToolResult{Content: "replacement", Details: JSON{"hooked": true}, Diagnostics: []ToolDiagnostic{{Severity: "info", Code: "hooked", Message: "seen"}}, StructuredOutput: JSON{"drop": true}, HasStructuredOutput: true, Usage: &goai.Usage{Input: 2, Output: 1, TotalTokens: 3}, Control: &ToolControl{Terminate: true}}, nil
		}, nil)
		h, c, sub, tool := mainCutoffHooksStart(t, f, b.store)
		store := mainAfterRecoveryCrash(t, h, b.store)
		// A newly installed registration supplies current limits, not the
		// old intent's defaults. The tool itself must never be redispatched.
		if err := f.options.Registry.Install(&Extension{Name: "current-limits", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "work", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "cutoff.result", Version: 1, OutputLimits: &ToolOutputLimits{MaxBytes: 4, MaxLines: 2000, Retain: "head"}, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
			t.Error("cutoff redispatched new tool")
			return ToolResult{}, nil
		}}}}); err != nil {
			t.Fatal(err)
		}
		h, c = mainCutoffHooksOpen(t, f, store, c.ID())
		mainCutoffHooksDone(t, h, c, sub, tool)
		receipt := mainCutoffHooksReceipt(t, h, tool)
		if calls.Load() != 1 || f.requests.Load() != 2 || f.runs.Load() != 1 {
			t.Error("hook/control execution", calls.Load(), f.requests.Load(), f.runs.Load())
		}
		// Native receipts append rendered diagnostics as a separate block.
		var text string
		if len(receipt.Content) > 0 {
			text = receipt.Content[0].Text
		}
		if text != "repl" || receipt.IsError || !equalJSONValue(receipt.Details, JSON{"hooked": true}) {
			t.Error("normalized replacement", receipt)
		}
		var codes []string
		for _, d := range receipt.Diagnostics {
			codes = append(codes, d.Code)
		}
		if !reflect.DeepEqual(codes, []string{"hooked", "truncated"}) {
			t.Error("hook/limit diagnostics", codes)
		}
		if receipt.Usage == nil || receipt.Usage.TotalTokens != 3 {
			t.Error("hook usage", receipt.Usage)
		}
		usage, err := c.Usage(bg)
		if err != nil || usage.Tools["work"].TotalTokens != 3 {
			t.Error("stored hook usage", usage, err)
		}
	})
}

// harness-tool-hooks.test.ts:443-463 and :508-538. Establish the live
// and stored progress boundaries before abort, then reopen for readback.
func TestSourceV160AbortHookPreservesTerminalToolResult(t *testing.T) {
	for _, mode := range []string{"throws", "held"} {
		t.Run(mode, func(t *testing.T) {
			store, dir := newSQLiteTestStore(t)
			failure := errors.New("hook failed")
			type hookReceipt struct {
				call   string
				result ToolResult
			}
			var hooks []hookReceipt
			var hookMu sync.Mutex
			f := mainCutoffHooksSetup(t, func(_ context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
				hookMu.Lock()
				hooks = append(hooks, hookReceipt{call.ID, result})
				hookMu.Unlock()
				if mode == "throws" {
					return nil, failure
				}
				return nil, nil
			}, nil)
			f.options.Settings.RestartLimit = nil
			if mode == "held" {
				window := float64(60_000)
				f.options.Settings.Progress.PersistWithinMs = &window
			}
			h := openHarness(t, store, f.options)
			c := root(t, h, f.ref)
			sub, err := c.Submit(bg, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, f.started)
			wantOutput := "work " + strings.Repeat("o", 40) + "\n"
			if mode == "held" {
				live, found, err := c.SnapshotDefinition(bg, LiveDoc(), nil)
				if err != nil || !found {
					t.Fatal("live tool progress", live, found, err)
				}
				slots, _ := generationObject(live.Copy())["tools"].([]any)
				if len(slots) != 1 || generationObject(slots[0])["output"] != wantOutput || !equalJSONValue(generationObject(slots[0])["details"], JSON{"run": 1}) {
					t.Fatal("held tool progress missing from Harness", slots)
				}
				address := DocumentAddress{Scope: "conversation", Owner: c.ID(), Kind: "pi.live"}
				doc, found, err := store.FindDocument(bg, address, CurrentDocumentPoint())
				if err != nil || !found {
					t.Fatal("stored live document", doc, found, err)
				}
				doc, found, err = store.DocumentAt(bg, doc.ID, CurrentDocumentPoint())
				if err != nil || !found {
					t.Fatal("stored live value", doc, found, err)
				}
				stored, _ := doc.Value["tools"].([]any)
				if len(stored) != 1 || generationObject(stored[0])["details"] != nil || generationObject(stored[0])["output"] == wantOutput {
					t.Fatal("progress stored before abort", stored)
				}
			}
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			if err := c.Abort(ctx); err != nil {
				t.Fatal(err)
			}
			if record := waitSubmission(t, sub).Record; record.Status != "unanswered" {
				t.Fatal("abort settlement", record)
			}
			if liveResult := v160ToolResult(t, bg, c, "c1"); !liveResult.IsError || !strings.HasPrefix(v160ToolResultText(liveResult), strings.TrimSuffix(wantOutput, "\n")) {
				t.Fatal("abort result lost original output", liveResult)
			}
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			hookMu.Lock()
			seen := append([]hookReceipt(nil), hooks...)
			hookMu.Unlock()
			if len(seen) != 1 || seen[0].call != "c1" || !reflect.DeepEqual(mainCutoffCodes(seen[0].result), []string{"aborted"}) {
				t.Fatal("abort hook did not receive one built result", seen)
			}
			if result := seen[0].result; !result.HasDetails || !equalJSONValue(result.DetailsValue, JSON{"run": 1}) || result.Content != wantOutput {
				t.Fatal("abort hook lost held output or details", result)
			}
			if mode == "throws" {
				f.mu.Lock()
				reported := append([]error(nil), f.errors...)
				f.mu.Unlock()
				if len(reported) != 1 || !errors.Is(reported[0], failure) || reported[0].Error() != "hook failed" {
					t.Fatalf("throwing afterTool report: %v", reported)
				}
			}
			reader, err := openSQLiteTestDirectory(dir, SQLiteOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close(bg)
			viewer, err := Open(bg, reader, Options{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer viewer.Close(bg)
			view, err := viewer.Conversation(bg, c.ID())
			if err != nil || view == nil {
				t.Fatal(view, err)
			}
			persisted := v160ToolResult(t, bg, view, "c1")
			if !persisted.IsError || !equalJSONValue(persisted.Details, JSON{"run": 1}) || !strings.HasPrefix(v160ToolResultText(persisted), strings.TrimSuffix(wantOutput, "\n")) {
				t.Fatal("reopened terminal tool receipt", persisted)
			}
			page, err := view.Entries(bg, EntryQuery{}, 10, nil)
			if err != nil || page.Next != nil {
				t.Fatal(page, err)
			}
			var codes []string
			for _, entry := range page.Items {
				if entry.Kind != toolResultEntryKind {
					continue
				}
				diagnostics, _ := generationObject(entry.Data)["diagnostics"].([]any)
				for _, diagnostic := range diagnostics {
					code, _ := generationObject(diagnostic)["code"].(string)
					codes = append(codes, code)
				}
			}
			if !reflect.DeepEqual(codes, []string{"aborted"}) {
				t.Fatal("durable abort entry diagnostics", codes)
			}
		})
	}
}

func TestMainCutoffHooksThrowReportsAndRetainsSlot(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		failure := errors.New("cutoff hook failed")
		var calls atomic.Int64
		f := mainCutoffHooksSetup(t, func(context.Context, goai.ToolCall, ToolResult, *HookAPI) (*ToolResult, error) {
			calls.Add(1)
			return nil, failure
		}, nil)
		h, c, sub, tool := mainCutoffHooksStart(t, f, b.store)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, c = mainCutoffHooksOpen(t, f, store, c.ID())
		mainCutoffHooksDone(t, h, c, sub, tool)
		receipt := mainCutoffHooksReceipt(t, h, tool)
		if len(receipt.Content) == 0 || !strings.HasPrefix(receipt.Content[0].Text, "work "+strings.Repeat("o", 40)) || !receipt.IsError {
			t.Error("slot fallback", receipt)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		reported := append([]error(nil), f.errors...)
		infos := append([]ReportInfo(nil), f.infos...)
		f.mu.Unlock()
		if len(infos) != 1 || infos[0].Extension != "cutoff-result" || infos[0].Task == nil || infos[0].Task.ID != tool {
			t.Error("extension attribution", infos)
		}
		found := false
		for _, err := range reported {
			if errors.Is(err, failure) {
				found = true
			}
		}
		if calls.Load() != 1 || !found {
			t.Error("hook throw not attributed", calls.Load(), reported)
		}
	})
}
