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
	err := f.options.Registry.Install(&Extension{Name: "cutoff-result", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "work", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "cutoff.result", Version: 1, OutputLimits: limits, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
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
	if _, err := c.Commit(bg, func(tx *Tx) error {
		doc, err := builtin(tx, c.ID(), "pi.live")
		if err != nil {
			return err
		}
		_, err = doc.Get()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := store.Snapshot(bg)
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
		if err := f.options.Registry.Install(&Extension{Name: "current-limits", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "work", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "cutoff.result", Version: 1, OutputLimits: &ToolOutputLimits{MaxBytes: 4, MaxLines: 2000, Retain: "head"}, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
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
