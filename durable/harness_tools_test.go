package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func registerSum(t *testing.T, registry *Registry, effects *atomic.Int64, document ID) {
	t.Helper()
	e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "sum", Description: "Add two bounded integers", Parameters: json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer","minimum":0,"maximum":100},"b":{"type":"integer","minimum":0,"maximum":100}},"required":["a","b"],"additionalProperties":false}`)}, Implementation: "sum.native", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		a, _ := asNumber(args["a"])
		b, _ := asNumber(args["b"])
		if e := api.Output("computed:"); e != nil {
			return ToolResult{}, e
		}
		return ToolResult{Content: fmt.Sprint(a + b), Details: JSON{"sum": a + b}, Usage: &goai.Usage{Output: 1, TotalTokens: 1}, Commit: func(tx *Tx) error {
			h, e := tx.documentRecord(document)
			if e != nil {
				return e
			}
			return h.Set(JSON{"sum": a + b})
		}}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
}
func createAppDocument(t *testing.T, r *ConversationHandle) ID {
	t.Helper()
	var id ID
	_, e := r.Commit(bg, func(tx *Tx) (any, error) {
		var e error
		id, e = tx.mintID()
		if e != nil {
			return nil, e
		}
		_, e = tx.createDocumentRecord(Document{ID: id, Scope: "conversation", Owner: r.ID(), Kind: "app.result", Version: 1, Value: JSON{"sum": 0}})
		return nil, e
	})
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func toolAnswer(callID, name string, args JSON) *goai.DoneEvent {
	return &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: callID, Name: name, Arguments: args}}, StopReason: goai.StopReasonToolUse, Usage: &goai.Usage{Input: 3, Output: 2, TotalTokens: 5}}}
}
func TestM1cActualHTTPToolResultAwareAnswer(t *testing.T) {
	var requests, effects atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		n := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			tools, ok := body["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Error("offered tools missing", body)
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"repeated-provider-id\",\"type\":\"function\",\"function\":{\"name\":\"sum\",\"arguments\":\"{\\\"a\\\":2,\\\"b\\\":3}\"}}]},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n")
		} else {
			messages := body["messages"].([]any)
			found := false
			for i, m := range messages {
				v := m.(map[string]any)
				if v["role"] == "tool" {
					found = true
					if i == 0 || messages[i-1].(map[string]any)["role"] != "assistant" || v["tool_call_id"] != "repeated-provider-id" || v["content"] != "5" {
						t.Error("call/result pairing", messages)
					}
				}
			}
			if !found {
				t.Error("model did not see owned result")
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"the sum is 5\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	registry := NewRegistry()
	model := fakeModel(goai.ApiOpenAICompletions)
	model.BaseURL = server.URL
	dir := filepath.Join(t.TempDir(), "private")
	store, e := openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h := openHarness(t, store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "fake-secret"}, nil
	}})
	r := root(t, h, ModelRef{model.Provider, model.ID})
	doc := createAppDocument(t, r)
	registerSum(t, registry, &effects, doc)
	sub, e := r.Submit(bg, Input{Content: "add 2 and3", RequestID: "tool-request"})
	if e != nil {
		t.Fatal(e)
	}
	result := waitSubmission(t, sub)
	if result.Submission.Status != "done" || result.Message.Content[0].Text != "the sum is 5" || requests.Load() != 2 || effects.Load() != 1 {
		t.Fatal("useful tool chain", result, requests.Load(), effects.Load())
	}
	snapshot, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	if snapshot.Documents[doc].Value["sum"] != json.Number("5") {
		t.Fatal("application result absent")
	}
	var parent, child Task
	for _, task := range snapshot.Tasks {
		if task.Kind == "pi.generation" {
			parent = task
		} else if task.Kind == "pi.tool" {
			child = task
		}
	}
	if child.Owner == 0 || snapshot.Tasks[child.Owner].Kind != "pi.generation" || snapshot.Tasks[child.Owner].Status != "done" || parent.Status != "done" || child.Status != "done" {
		t.Fatal("owned drain")
	}
	assertToolUsageReceipts(t, snapshot, "sum", &goai.Usage{Output: 1, TotalTokens: 1}, h.session.limits)
	for _, d := range snapshot.Documents {
		if d.Kind == "pi.usage" {
			models := d.Value["models"].(map[string]any)
			tools := d.Value["tools"].(map[string]any)
			var usage goai.Usage
			if e = fromObject(JSON(models["openai/durable-test"].(map[string]any)), &usage, h.session.limits); e != nil || usage.TotalTokens != 13 {
				t.Fatal("model attempt accounting", usage, e)
			}
			if _, ok := tools["sum"]; !ok {
				t.Fatal("tool usage missing", tools)
			}
		}
	}
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	store, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
	rr, e := h2.Conversation(bg, 1)
	if e != nil {
		t.Fatal(e)
	}
	again, e := rr.Submit(bg, Input{RequestID: "tool-request"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, again).Submission.Status != "done" || effects.Load() != 1 || requests.Load() != 2 {
		t.Fatal("committed tool chain redispatched")
	}
	reopened, e := h2.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	assertToolUsageReceipts(t, reopened, "sum", &goai.Usage{Output: 1, TotalTokens: 1}, h2.session.limits)
}

func TestM1cSchemaSubsetAndRepairBeforeIntent(t *testing.T) {
	for _, schema := range []string{`{}`, `{"type":"object","unknown":true}`, `{"type":"object","required":["absent"]}`, `{"type":"array"}`, `{"type":"string","pattern":"["}`, `{"type":"number","minimum":10,"maximum":1}`} {
		registry := NewRegistry()
		e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "test", Parameters: json.RawMessage(schema)}, Implementation: "test.impl", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			t.Error("invalid schema effect")
			return ToolResult{}, nil
		}})
		if e == nil {
			t.Fatal("malformed/unsupported schema accepted", schema)
		}
	}
	var effects, repairs atomic.Int64
	registry := NewRegistry()
	e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "repair", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","minimum":1,"maximum":9}},"required":["n"],"additionalProperties":false}`)}, Implementation: "repair.impl", Version: 1, ReplaySafe: true, Validator: func(context.Context, JSON) (JSON, error) { repairs.Add(1); return JSON{"n": 3}, nil }, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		if args["n"] != json.Number("3") {
			t.Error("rewritten args not stored")
		}
		state, e := api.h.Snapshot(bg)
		if e != nil {
			t.Error(e)
		}
		var cp toolCheckpoint
		if e = fromObject(state.Tasks[api.TaskID()].Checkpoint, &cp, api.h.session.limits); e != nil || cp.Arguments["n"] != json.Number("3") || !cp.Started {
			t.Error("effect before final intent", cp, e)
		}
		return ToolResult{Content: "3"}, nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	var rounds atomic.Int64
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, conv *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if rounds.Add(1) == 1 {
			ch <- toolAnswer("same-call", "repair", JSON{"n": 2})
		} else {
			assertOriginalCallAndResult(t, conv, "repair", "same-call", "n", json.Number("2"), "3")
			ch <- terminal("answer")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	s, _ := newTestSQLite(t)
	h := openHarness(t, s, options)
	sub, e := root(t, h, ref).Submit(bg, Input{Content: "repair"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, sub).Submission.Status != "done" || effects.Load() != 1 || repairs.Load() != 2 {
		t.Fatal("repair/effectcount")
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	assertStoredOriginalAndExecutionArgs(t, state, "repair", "n", json.Number("2"), json.Number("3"), h.session.limits)
}

func TestM1cNotOfferedAndInvalidArgumentsZeroEffects(t *testing.T) {
	for _, mode := range []string{"not-offered", "wrongtype", "maximum", "additional", "required"} {
		t.Run(mode, func(t *testing.T) {
			var effects, rounds atomic.Int64
			registry := NewRegistry()
			registerSum(t, registry, &effects, 0)
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if rounds.Add(1) == 1 {
					args := JSON{"a": 2, "b": 3}
					name := "sum"
					switch mode {
					case "not-offered":
						name = "secret"
					case "wrongtype":
						args["a"] = "2"
					case "maximum":
						args["a"] = 101
					case "additional":
						args["extra"] = true
					case "required":
						delete(args, "b")
					}
					ch <- toolAnswer("call", name, args)
				} else {
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			s, _ := newTestSQLite(t)
			h := openHarness(t, s, options)
			sub, e := root(t, h, ref).Submit(bg, Input{Content: "bad"})
			if e != nil {
				t.Fatal(e)
			}
			if waitSubmission(t, sub).Submission.Status != "done" || effects.Load() != 0 {
				t.Fatal("unoffered/invalid effect")
			}
			state, e := h.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			for _, task := range state.Tasks {
				if task.Kind == "pi.tool" {
					var cp toolCheckpoint
					_ = fromObject(task.Checkpoint, &cp, h.session.limits)
					if cp.Result == nil || !cp.Result.IsError {
						t.Fatal("missing explicit tool error")
					}
				}
			}
		})
	}
}

func TestM1cAbortDrainsChildBeforeParentAndFencesEscapedAPI(t *testing.T) {
	var effects atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	var escaped *ToolAPI
	registry := NewRegistry()
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "block", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "block.impl", Version: 1, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		escaped = api
		if e := api.Output("retained prefix"); e != nil {
			return ToolResult{}, e
		}
		if err := api.Details(JSON{"progress": true}); err != nil {
			return ToolResult{}, err
		}
		close(entered)
		<-release
		return ToolResult{Content: "ignored afterabort", Usage: &goai.Usage{Input: 1, TotalTokens: 1}, Commit: func(*Tx) error { t.Error("aborted application effect"); return nil }}, nil
	}}); e != nil {
		t.Fatal(e)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- toolAnswer("call", "block", JSON{})
		close(ch)
		return ch
	})
	options.Registry = registry
	s, dir := newSQLiteTestStore(t)
	h := openHarness(t, s, options)
	cleanupTaskGates(t, release)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "block"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	aborted := make(chan error, 1)
	go func() { aborted <- r.Abort(ctx) }()
	// Observe durable marks and the parent Hold before cancelling the caller.
	// A short elapsed-time deadline can expire BEFORE admission under -race.
	deadline, stop := context.WithTimeout(bg, 3*time.Second)
	defer stop()
	for {
		state, err := h.Snapshot(deadline)
		if err != nil {
			t.Fatal(err)
		}
		parentHeld, childMarked := false, false
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" {
				parentHeld = task.Status == "completing" && taskAborted(task)
			} else if task.Kind == "pi.tool" {
				childMarked = taskAborted(task)
			}
		}
		if parentHeld && childMarked {
			break
		}
		select {
		case err := <-aborted:
			t.Fatal("Abort returned before noncooperative host", err)
		case <-deadline.Done():
			t.Fatal("Abort marks/Hold not adopted", deadline.Err())
		default:
			runtime.Gosched()
		}
	}
	cancel()
	if e = <-aborted; !errors.Is(e, context.Canceled) {
		t.Fatal("Abort failed towaitnoncooperative", e)
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	var parent Task
	for _, task := range state.Tasks {
		if task.Kind == "pi.generation" {
			parent = task
			if task.Status != "completing" {
				t.Fatal("prematureparentterminal")
			}
		} else {
			var cp toolCheckpoint
			_ = fromObject(task.Checkpoint, &cp, h.session.limits)
			cp, e = toolPresentation(state, task, cp, h.session.limits)
			if e != nil {
				t.Fatal(e)
			}
			if !cp.Abort || cp.Output != "retained prefix" {
				t.Fatal("abort/prefix notcommitted")
			}
		}
	}
	if parent.ID == 0 {
		t.Fatal("parentmissing")
	}
	if e = escaped.Output("late"); e == nil {
		t.Fatal("outputafterabort")
	}
	close(release)
	if waitSubmission(t, sub).Submission.Status != "aborted" {
		t.Fatal("abort notsettled")
	}
	if e = escaped.Output("postreturn"); !errors.Is(e, ErrSealed) {
		t.Fatal(e)
	}
	state, e = h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	childTerminalSeq := uint64(0)
	for _, entry := range state.Entries {
		if legacyEntryValue(entry)["role"] == string(goai.RoleToolResult) {
			childTerminalSeq = entry.Seq
		}
		if legacyEntryValue(entry)["role"] == string(goai.RoleAssistant) && legacyEntryValue(entry)["errorCode"] == "aborted" {
			t.Fatal("abort invented assistant receipt", entry)
		}
	}
	parentTerminalSeq := state.Seq // Last commit settles generation/submission after tool receipt.
	if childTerminalSeq == 0 || parentTerminalSeq <= childTerminalSeq || state.Tasks[parent.ID].Status != "aborted" {
		t.Fatal("notbottomup", childTerminalSeq, parentTerminalSeq)
	}
	if effects.Load() != 1 {
		t.Fatal("extraeffect")
	}
	assertToolUsageReceipts(t, state, "block", &goai.Usage{Input: 1, TotalTokens: 1}, h.session.limits)
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	s, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, s, options)
	resumed, e := h2.Submission(bg, sub.ID())
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, resumed).Submission.Status != "aborted" || effects.Load() != 1 {
		t.Fatal("aborted tool replay")
	}
	state, e = h2.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	assertToolUsageReceipts(t, state, "block", &goai.Usage{Input: 1, TotalTokens: 1}, h2.session.limits)
}

func TestM1cCloseNoncooperativeToolKeepsOwnershipAndReturnedOutcome(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var escaped *ToolAPI
	registry := NewRegistry()
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "block", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "block.impl", Version: 1, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
		escaped = api
		close(entered)
		<-release
		return ToolResult{Content: "finished"}, nil
	}}); e != nil {
		t.Fatal(e)
	}
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("call", "block", JSON{})
		} else {
			ch <- terminal("answer")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	s, dir := newSQLiteTestStore(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "close"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	ctx, cancel := context.WithTimeout(bg, 30*time.Millisecond)
	defer cancel()
	if e = h.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, e = openSQLiteTestDirectory(dir, SQLiteOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal("prematuresuccessor", e)
	}
	close(release)
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	if e = escaped.Output("late"); !errors.Is(e, ErrSealed) {
		t.Fatal(e)
	}
	s, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, s, options)
	state, e := h2.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range state.Tasks {
		if task.Kind == "pi.tool" && task.Status != "done" {
			t.Fatal("returned outcome lost")
		}
	}
	resumed, e := h2.Submission(bg, sub.ID())
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, resumed).Submission.Status != "done" || requests.Load() != 2 {
		t.Fatal("tool replay orlostsuccessor")
	}
}

func TestM1cRegistryPhasePinOutputCapAndAtomicAppFailure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var oldCalls, newCalls atomic.Int64
	registry := NewRegistry()
	reg := ToolRegistration{Definition: goai.Tool{Name: "pin", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "pin.impl", Version: 1, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
		oldCalls.Add(1)
		if e := api.Output(strings.Repeat("x", MaxToolOutputBytes+1)); e != nil {
			return ToolResult{}, e
		}
		return ToolResult{Content: "ok", Commit: func(tx *Tx) error { return reject("applicationfailure") }}, nil
	}}
	if e := registry.Register(reg); e != nil {
		t.Fatal(e)
	}
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event)
		n := requests.Add(1)
		go func() {
			if n == 1 {
				close(entered)
				<-release
				ch <- toolAnswer("call", "pin", JSON{})
			} else {
				ch <- terminal("answer")
			}
			close(ch)
		}()
		return ch
	})
	options.Registry = registry
	s, _ := newTestSQLite(t)
	h := openHarness(t, s, options)
	sub, e := root(t, h, ref).Submit(bg, Input{Content: "phasepin"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	reg.Execute = func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
		newCalls.Add(1)
		if err := api.Output(strings.Repeat("x", MaxToolOutputBytes+1)); err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Content: "ok", Commit: func(*Tx) error { return reject("applicationfailure") }}, nil
	}
	if e = registry.Register(reg); e != nil {
		t.Fatal(e)
	}
	close(release)
	if waitSubmission(t, sub).Submission.Status != "done" || oldCalls.Load() != 0 || newCalls.Load() != 1 {
		t.Fatal("tool call did not resolve its current phase implementation")
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range state.Tasks {
		if task.Kind == "pi.tool" {
			var cp toolCheckpoint
			_ = fromObject(task.Checkpoint, &cp, h.session.limits)
			if cp.ErrorCode != "tool_outcome_rejected" {
				t.Fatal("appfailure/output notatomic", cp)
			}
		}
	}
}

func TestM1cCommittedPartialAndQueuedWithdrawal(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	committed := make(chan struct{})
	var acknowledge, releaseOnce sync.Once
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event)
		go func() {
			m := terminal("bounded prefix").Message
			ch <- &goai.TextDeltaEvent{Partial: m, Delta: "bounded prefix"}
			close(entered)
			<-release
			ch <- terminal("answer")
			close(ch)
		}()
		return ch
	})
	s, _ := newTestSQLite(t)
	// Signal from the native append settlement barrier. Snapshot then joins
	// the still-held session line and therefore reads the adopted revision.
	s.appendFrame = func(typ byte, ordinal, high uint64, p []byte) error {
		if typ == 2 && bytes.Contains(p, []byte(`"bounded prefix"`)) {
			acknowledge.Do(func() { close(committed) })
		}
		return nil
	}
	h := openHarness(t, s, options)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	r := root(t, h, ref)
	first, e := r.Submit(bg, Input{Content: "active", RequestID: "active"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	<-committed
	queued, e := r.Submit(bg, Input{Content: "withdraw", RequestID: "withdraw"})
	if e != nil {
		t.Fatal(e)
	}
	if e = queued.Withdraw(bg); e != nil {
		t.Fatal(e)
	}
	if e = first.Withdraw(bg); e == nil {
		t.Fatal("activeinputwithdrawn")
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	partialFound := false
	for _, task := range state.Tasks {
		var cp generationCheckpoint
		_ = fromObject(task.Checkpoint, &cp, h.session.limits)
		if cp.Submission == first.ID() {
			partial, err := generationPartial(state, task, cp, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			partialFound = partial != nil && partial.Content[0].Text == "bounded prefix"
		}
	}
	if !partialFound {
		t.Fatal("partial notcommitted")
	}
	if state.Submissions[queued.ID()].Status != "aborted" {
		t.Fatal("withdrawnotdurable")
	}
	releaseOnce.Do(func() { close(release) })
	if waitSubmission(t, first).Submission.Status != "done" || waitSubmission(t, queued).Submission.Status != "aborted" || calls.Load() != 1 {
		t.Fatal("withdrawstartedmodel")
	}
}

func TestM1cSupportedSchemaConstraintsAndHostValidator(t *testing.T) {
	schemas := []struct {
		schema string
		bad    JSON
	}{{`{"type":"object","properties":{"v":{"type":"string","minLength":2,"maxLength":4,"pattern":"^[a-z]+$"}},"required":["v"]}`, JSON{"v": "12"}}, {`{"type":"object","properties":{"v":{"type":"array","items":{"type":"boolean"},"minItems":1,"maxItems":2}}}`, JSON{"v": []any{true, false, true}}}, {`{"type":"object","properties":{"v":{"type":"string","enum":["a","b"]}}}`, JSON{"v": "c"}}, {`{"type":"object","properties":{"v":{"type":"number","minimum":1,"maximum":2}}}`, JSON{"v": 0.5}}}
	for _, tc := range schemas {
		var schema JSON
		if e := decodeStrict([]byte(tc.schema), DefaultLimits(), 4096, &schema); e != nil {
			t.Fatal(e)
		}
		if e := validateSchema(schema, 1); e != nil {
			t.Fatal(e)
		}
		args, e := copyObject(tc.bad, DefaultLimits())
		if e != nil {
			t.Fatal(e)
		}
		if e = checkSchema(schema, args, 1); e == nil {
			t.Fatal("constraintnotchecked", tc.schema)
		}
	}
	registry := NewRegistry()
	calls := 0
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "host", Parameters: json.RawMessage(`{"type":"object","oneOf":[{"required":["x"]}]}`)}, Implementation: "host.impl", Version: 1, Validator: func(context.Context, JSON) (JSON, error) { calls++; return nil, errors.New("strict host reject") }, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
		t.Error("hostrejecteffect")
		return ToolResult{}, nil
	}}); e != nil {
		t.Fatal(e)
	}
	reg, _ := registry.current("host")
	if _, e := validateArguments(bg, reg, JSON{}, DefaultLimits()); e == nil || calls != 1 {
		t.Fatal("strictvalidator notenforced")
	}
}

func TestM1cExactNumericSchemaAndMalformedHostBoundary(t *testing.T) {
	l := DefaultLimits()
	var schema JSON
	if e := decodeStrict([]byte(`{"type":"object","properties":{"n":{"type":"integer","maximum":9007199254740991}}}`), l, 4096, &schema); e != nil {
		t.Fatal(e)
	}
	for _, value := range []json.Number{"9007199254740990.5", "9007199254740991.1", "1e999999"} {
		args, e := copyObject(JSON{"n": value}, l)
		if e != nil {
			continue
		}
		if e = checkSchema(schema, args, 1); e == nil {
			t.Fatal("fraction/overflowroundedaccepted", value)
		}
	}
	args, e := copyObject(JSON{"n": json.Number("9007199254740991")}, l)
	if e != nil || checkSchema(schema, args, 1) != nil {
		t.Fatal("lastvalidinteger")
	}
	malformed := NewRegistry()
	if e = malformed.Register(ToolRegistration{Definition: goai.Tool{Name: "malformed", Parameters: json.RawMessage(`{"type":"object","required":true,"unknown":true}`)}, Implementation: "malformed.impl", Version: 1, Validator: func(context.Context, JSON) (JSON, error) { return JSON{}, nil }, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); e == nil {
		t.Fatal("hostvalidatorwaivedmalformedknownschema")
	}
}

func TestM1cInvalidToolUsageSettlesOnceNoAppOrReplay(t *testing.T) {
	for _, mode := range []string{"negative-input", "negative-cost", "nan-cost", "inf-cost", "overflow", "valid-error"} {
		t.Run(mode, func(t *testing.T) {
			var effects, apps, requests atomic.Int64
			registry := NewRegistry()
			if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "usage", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "usage.impl", Version: 1, ReplaySafe: true, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				usage := &goai.Usage{Input: 2, Output: 1, TotalTokens: 3}
				switch mode {
				case "negative-input":
					usage.Input = -1
				case "negative-cost":
					usage.Cost.Input = -1
				case "nan-cost":
					usage.Cost.Total = math.NaN()
				case "inf-cost":
					usage.Cost.Output = math.Inf(1)
				case "overflow":
					usage.Input = int(MaxID) + 1
				}
				result := ToolResult{Content: "result", Usage: usage, Commit: func(*Tx) error { apps.Add(1); return nil }}
				if mode == "valid-error" {
					return result, errors.New("SECRET_ERROR")
				}
				return result, nil
			}}); e != nil {
				t.Fatal(e)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					ch <- toolAnswer("call", "usage", JSON{})
				} else {
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			s, dir := newSQLiteTestStore(t)
			h := openHarness(t, s, options)
			r := root(t, h, ref)
			sub, e := r.Submit(bg, Input{Content: "usage", RequestID: "usage"})
			if e != nil {
				t.Fatal(e)
			}
			if waitSubmission(t, sub).Submission.Status != "done" || effects.Load() != 1 || apps.Load() != 0 {
				t.Fatal("invalidusage notsettledonce")
			}
			state, e := h.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			for _, task := range state.Tasks {
				if task.Kind == "pi.tool" {
					var cp toolCheckpoint
					_ = fromObject(task.Checkpoint, &cp, h.session.limits)
					if mode == "valid-error" {
						if task.Status != "done" || cp.ErrorCode != "tool_error" || cp.Result == nil {
							t.Fatal("executor throw result", task, cp)
						}
					} else {
						record, err := CanonicalTask(task, h.session.limits)
						if err != nil || record.State.Outcome.Status != "faulted" || record.State.Outcome.Error.Message != "invalid_usage" || cp.Result != nil {
							t.Fatal("invalid result did not scheduler-fault without receipt", record, cp, err)
						}
					}
				}
			}
			for _, doc := range state.Documents {
				if doc.Kind == "pi.usage" {
					tools := doc.Value["tools"].(map[string]any)
					if mode == "valid-error" {
						var usage goai.Usage
						value, ok := tools["usage"].(map[string]any)
						if !ok {
							t.Fatal("validbilledusage lost")
						}
						if e = fromObject(JSON(value), &usage, h.session.limits); e != nil || usage.TotalTokens != 3 {
							t.Fatal(usage, e)
						}
					} else if len(tools) != 0 {
						t.Fatal("invalidusagesaved")
					}
				}
			}
			var expectedUsage *goai.Usage
			if mode == "valid-error" {
				expectedUsage = &goai.Usage{Input: 2, Output: 1, TotalTokens: 3}
			}
			if mode == "valid-error" {
				assertToolUsageReceipts(t, state, "usage", expectedUsage, h.session.limits)
			} else {
				for _, entry := range state.Entries {
					var receipt MessageReceipt
					if isMessageEntry(entry) && decodeEntryMessage(entry, h.session.limits, &receipt) == nil && receipt.Role == goai.RoleToolResult && receipt.ToolName == "usage" {
						t.Fatal("invalid tool result invented transcript receipt", receipt)
					}
				}
			}
			_ = h.Close(bg)
			s, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
			if e != nil {
				t.Fatal(e)
			}
			h2 := openHarness(t, s, options)
			rr, e := h2.Conversation(bg, 1)
			if e != nil {
				t.Fatal(e)
			}
			again, e := rr.Submit(bg, Input{RequestID: "usage"})
			if e != nil {
				t.Fatal(e)
			}
			if waitSubmission(t, again).Submission.Status != "done" || effects.Load() != 1 || requests.Load() != 2 {
				t.Fatal("invalidusage replay")
			}
			state, e = h2.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "valid-error" {
				assertToolUsageReceipts(t, state, "usage", expectedUsage, h2.session.limits)
			} else {
				for _, task := range state.Tasks {
					if task.Kind == "pi.tool" {
						var cp toolCheckpoint
						if err := fromObject(task.Checkpoint, &cp, h2.session.limits); err != nil || cp.Result != nil {
							t.Fatal("faulted result appeared on reopen", cp, err)
						}
					}
				}
			}
		})
	}
}

func TestM1cRetryConfigRejectBeforeIntentAndRecovery(t *testing.T) {
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event, 1)
		ch <- terminal("answer")
		close(ch)
		return ch
	})
	invalid := true
	options.RequestOptions = func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		if invalid {
			return &goai.StreamOptions{RetryConfig: &goai.RetryConfig{MaxRetries: 99}}, nil
		}
		return nil, nil
	}
	s, _ := newTestSQLite(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "invalidretry"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, sub).Submission.Status != "failed" || calls.Load() != 0 {
		t.Fatal("unpinnedretryeffect")
	}
	invalid = false
	next, e := r.Submit(bg, Input{Content: "validafterreject"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, next).Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("rejectionpoisonedharness")
	}
}

func TestM1cRepeatedProviderCallIDUsesDistinctDurableTasks(t *testing.T) {
	var effects, requests atomic.Int64
	registry := NewRegistry()
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "repeat", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "repeat.impl", Version: 1, ReplaySafe: true, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		return ToolResult{Content: "ok"}, nil
	}}); e != nil {
		t.Fatal(e)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) <= 2 {
			ch <- toolAnswer("same-call-id", "repeat", JSON{})
		} else {
			ch <- terminal("answer")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	s, _ := newTestSQLite(t)
	h := openHarness(t, s, options)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		t.Logf("repeated call-ID progress: provider requests=%d tool effects=%d", requests.Load(), effects.Load())
		ctx, cancel := context.WithTimeout(bg, time.Second)
		defer cancel()
		state, err := s.Snapshot(ctx)
		if err != nil {
			t.Log("failed-test snapshot:", err)
			return
		}
		for _, id := range ids(state.Tasks) {
			task := state.Tasks[id]
			t.Logf("task %d %s status=%s phase=%v owner=%d", id, task.Kind, task.Status, task.Checkpoint["phase"], task.Owner)
		}
	})
	sub, e := root(t, h, ref).Submit(bg, Input{Content: "two rounds"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, sub).Submission.Status != "done" || effects.Load() != 2 {
		t.Fatal("callIDdedupcollided")
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	children := map[ID]bool{}
	for _, task := range state.Tasks {
		if task.Kind == "pi.tool" {
			children[task.ID] = true
		}
	}
	if len(children) != 2 {
		t.Fatal("durabletasksnotunique")
	}
}

func TestM1cProviderPartialPointersObservedDetachedAtBoundary(t *testing.T) {
	ch := make(chan goai.Event)
	message := terminal("committed prefix").Message
	s, _ := newTestSQLite(t)
	h := &Harness{session: sessionFor(t, s)}
	conversation, e := h.session.MintID(bg)
	if e != nil {
		t.Fatal(e)
	}
	id, e := h.session.MintID(bg)
	if e != nil {
		t.Fatal(e)
	}
	cp, e := dtoObject(generationCheckpoint{Phase: "intent", Submission: 1}, h.session.limits)
	if e != nil {
		t.Fatal(e)
	}
	_, e = h.session.Commit(bg, func(tx *Tx) (any, error) {
		if err := tx.createConversationRecord(Conversation{ID: conversation}); err != nil {
			return nil, err
		}
		if err := initializeBuiltins(tx, conversation); err != nil {
			return nil, err
		}
		live, err := builtin(tx, conversation, "pi.live")
		if err != nil {
			return nil, err
		}
		if err := live.Update(func(value JSON) error {
			value["run"] = JSON{"task": id}
			return nil
		}); err != nil {
			return nil, err
		}
		return nil, tx.putTaskRecord(Task{ID: id, Conversation: conversation, Kind: "pi.generation", Status: "running", Checkpoint: cp})
	})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { h.drain(ch, fakeModel("partial-ownership"), id); close(done) }()
	ch <- &goai.TextDeltaEvent{Partial: message}
	// The next send establishes detached capture, not a checkpoint. Wait for
	// the trailing publisher's adopted live value before ending the stream.
	ch <- &goai.StartEvent{}
	deadline, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		state, err := h.session.Snapshot(deadline)
		if err != nil {
			t.Fatal(err)
		}
		var checkpoint generationCheckpoint
		if err := fromObject(state.Tasks[id].Checkpoint, &checkpoint, h.session.limits); err != nil {
			t.Fatal(err)
		}
		partial, err := generationPartial(state, state.Tasks[id], checkpoint, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if partial != nil {
			break
		}
		select {
		case <-deadline.Done():
			t.Fatal("partial publication missing")
		default:
			runtime.Gosched()
		}
	}
	message.Content[0].Text = "producer mutated"
	close(ch)
	<-done
	state, e := h.session.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	var stored generationCheckpoint
	if e = fromObject(state.Tasks[id].Checkpoint, &stored, h.session.limits); e != nil {
		t.Fatal(e)
	}
	partial, e := generationPartial(state, state.Tasks[id], stored, h.session.limits)
	if e != nil || partial == nil || partial.Content[0].Text != "committed prefix" {
		t.Fatal("partialalias", e)
	}
}

func TestM1cAbortAtOfferedCallBoundaryStartsNoTool(t *testing.T) {
	var effects atomic.Int64
	registry := NewRegistry()
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "effect", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "effect.impl", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { effects.Add(1); return ToolResult{}, nil }}); e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event)
		go func() { close(entered); <-release; ch <- toolAnswer("call", "effect", JSON{}); close(ch) }()
		return ch
	})
	options.Registry = registry
	s, _ := newTestSQLite(t)
	h := openHarness(t, s, options)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "abortboundary"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	aborted := make(chan error, 1)
	go func() { aborted <- r.Abort(ctx) }()
	deadline, stop := context.WithTimeout(bg, 3*time.Second)
	defer stop()
	for {
		state, err := h.Snapshot(deadline)
		if err != nil {
			t.Fatal(err)
		}
		marked := false
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" && taskAborted(task) {
				marked = true
			}
		}
		if marked {
			break
		}
		select {
		case err := <-aborted:
			t.Fatal("Abort returned before provider return", err)
		case <-deadline.Done():
			t.Fatal("Abort mark not adopted", deadline.Err())
		default:
			runtime.Gosched()
		}
	}
	cancel()
	if e = <-aborted; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	releaseOnce.Do(func() { close(release) })
	result := waitSubmission(t, sub)
	if result.Submission.Status != "aborted" || effects.Load() != 0 {
		t.Fatal("tool dispatchedafterabort")
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	// The terminal response arrived after abort was adopted. The reference
	// converts only a committed partial; it does not bill/publish this response.
	if result.Message != nil {
		t.Fatal("aborted terminal response published", result)
	}
	for _, doc := range state.Documents {
		if doc.Kind == "pi.usage" {
			models := doc.Value["models"].(map[string]any)
			if _, exists := models["openai/durable-test"]; exists {
				t.Fatal("uncommitted abort response billed", models)
			}
		}
	}
}

func assertOriginalCallAndResult(t *testing.T, conv *goai.Context, name, callID, key string, original any, result string) {
	t.Helper()
	paired := false
	for i, message := range conv.Messages {
		for _, call := range message.Content {
			if call.Type != "toolCall" || call.Name != name {
				continue
			}
			if call.ID != callID || !equalJSONValue(call.Arguments[key], original) {
				t.Error("model call provenance changed", call)
			}
			if i+1 >= len(conv.Messages) {
				t.Error("tool result missing after call")
				continue
			}
			next := conv.Messages[i+1]
			if next.Role != goai.RoleToolResult || next.ToolCallID != callID || next.ToolName != name || len(next.Content) != 1 || next.Content[0].Text != result {
				t.Error("successor call/result pairing", next)
			}
			paired = true
		}
	}
	if !paired {
		t.Error("successor original call absent")
	}
}
func assertStoredOriginalAndExecutionArgs(t *testing.T, state Snapshot, name, key string, original, execution any, l Limits) {
	t.Helper()
	calls, children := 0, 0
	for _, entry := range state.Entries {
		var receipt MessageReceipt
		if e := decodeEntryMessage(entry, l, &receipt); e != nil {
			t.Fatal(e)
		}
		if receipt.Role != goai.RoleAssistant {
			continue
		}
		for _, call := range receipt.Content {
			if call.Type == "toolCall" && call.Name == name {
				calls++
				if !equalJSONValue(call.Arguments[key], original) {
					t.Fatal("stored model call rewritten", call.Arguments)
				}
			}
		}
	}
	for _, task := range state.Tasks {
		if task.Kind != "pi.tool" {
			continue
		}
		var cp toolCheckpoint
		if e := fromObject(task.Checkpoint, &cp, l); e != nil {
			t.Fatal(e)
		}
		if cp.Offer.Name == name {
			children++
			if !equalJSONValue(cp.Arguments[key], execution) {
				t.Fatal("execution intent not repaired", cp.Arguments)
			}
		}
	}
	if calls != 1 || children != 1 {
		t.Fatal("call/intent identity counts", calls, children)
	}
}
func assertToolUsageReceipts(t *testing.T, state Snapshot, name string, want *goai.Usage, l Limits) {
	t.Helper()
	entries, children, ledgers := 0, 0, 0
	matches := func(usage *goai.Usage) {
		t.Helper()
		if (usage == nil) != (want == nil) {
			t.Fatal("tool usage receipt presence", usage, want)
		}
		if usage != nil {
			a, e := dtoObject(usage, l)
			if e != nil {
				t.Fatal(e)
			}
			b, e := dtoObject(want, l)
			if e != nil {
				t.Fatal(e)
			}
			if !equalJSONValue(a, b) {
				t.Fatal("tool usage receipt differs", usage, want)
			}
		}
	}
	for _, entry := range state.Entries {
		if !isMessageEntry(entry) {
			continue
		}
		var receipt MessageReceipt
		if e := decodeEntryMessage(entry, l, &receipt); e != nil {
			t.Fatal(e)
		}
		if receipt.Role == goai.RoleToolResult && receipt.ToolName == name {
			entries++
			matches(receipt.Usage)
		}
	}
	for _, task := range state.Tasks {
		if task.Kind != "pi.tool" {
			continue
		}
		var cp toolCheckpoint
		if e := fromObject(task.Checkpoint, &cp, l); e != nil {
			t.Fatal(e)
		}
		if cp.Offer.Name == name {
			children++
			if cp.Result == nil {
				t.Fatal("checkpoint tool receipt absent")
			}
			matches(cp.Result.Usage)
		}
	}
	for _, doc := range state.Documents {
		if doc.Kind != "pi.usage" {
			continue
		}
		tools, ok := doc.Value["tools"].(map[string]any)
		if !ok {
			t.Fatal("tool ledger shape")
		}
		raw, found := tools[name]
		if want == nil {
			if found {
				t.Fatal("invalid/absent usage aggregated")
			}
			continue
		}
		if !found {
			t.Fatal("valid usage not aggregated")
		}
		value, ok := raw.(map[string]any)
		if !ok {
			t.Fatal("ledger usage shape")
		}
		var usage goai.Usage
		if e := fromObject(JSON(value), &usage, l); e != nil {
			t.Fatal(e)
		}
		matches(&usage)
		ledgers++
	}
	if entries != 1 || children != 1 || (want != nil && ledgers != 1) {
		t.Fatal("tool receipt/accounting counts", entries, children, ledgers)
	}
}

// Source-derived from harness-tools.test.ts:942-948 api.commit result/attribution.
// Native AppendEntry still returns only an error; this callback returns its
// minted ID and inspects the adopted entry separately, leaving that API gap open.
func TestSourceToolAPICommitReturnsValuesAndAttributedEntry(t *testing.T) {
	type committer interface {
		Commit(context.Context, func(*Tx) (any, error), ...CommitOptions) (any, error)
	}
	for _, mode := range []string{"model", "nested"} {
		t.Run(mode, func(t *testing.T) {
			var effects atomic.Int64
			var escaped *ToolAPI
			var committed ID
			registry := NewRegistry()
			reg := wrapRegistration("commit_value_probe")
			reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				escaped = api
				values, ok := any(api).(committer)
				if !ok {
					t.Error("source ToolExecutionApi.commit callback-value signature missing")
					return ToolResult{IsError: true}, nil
				}
				value, err := values.Commit(ctx, func(tx *Tx) (any, error) {
					appendEntry, ok := any(tx.AppendEntry).(func(ID, JSON) (Entry, error))
					if !ok {
						return nil, errors.New("source draft/record append signature missing")
					}
					return appendEntry(api.runtime.ConversationID(), JSON{"kind": "test.note", "data": JSON{"probe": true}})
				})
				entry, ok := value.(Entry)
				committed = entry.ID
				if err != nil || !ok || committed == 0 || entry.ByTask != api.TaskID() || entry.Kind != "test.note" {
					t.Error("tool commit lost its constructor record/attribution", value, err)
				}
				marker := &struct{ Cycle any }{}
				marker.Cycle = marker
				value, err = values.Commit(ctx, func(*Tx) (any, error) { return marker, nil })
				if value != marker || err != nil {
					t.Error("tool commit copied or replaced host identity", value, err)
				}
				failure := errors.New("tool callback failure")
				value, err = values.Commit(ctx, func(*Tx) (any, error) { return marker, failure })
				if value != nil || err != failure {
					t.Error("tool callback failure lost identity or leaked value", value, err)
				}
				return ToolResult{Content: "value committed"}, nil
			}
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				last := input.Messages[len(input.Messages)-1]
				if last.Role == goai.RoleUser {
					answer := terminal("call")
					answer.Message.StopReason = goai.StopReasonToolUse
					answer.Reason = goai.StopReasonToolUse
					answer.Message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "source-value", Name: "commit_value_probe", Arguments: map[string]any{}}}
					ch <- answer
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			definition := taskDefinition(t, "source.tool-api-values", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				result, err := r.CallTool(ctx, "probe", "commit_value_probe", JSON{})
				if err != nil || result.IsError || result.Content != "value committed" {
					t.Error("source nested tool commit result", result, err)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			if _, err := registry.RegisterTask(definition); err != nil {
				t.Fatal(err)
			}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			if mode == "model" {
				submission, err := conversation.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				waitSubmission(t, submission)
			} else {
				record := waitPublicTask(t, h, createPublicTask(t, h, definition, nil, TaskOptions{Conversation: conversation.ID(), Ownership: TaskOwnership{Kind: "conversation"}}))
				if record.State.Outcome.Status != "completed" {
					t.Error(record.State.Outcome)
				}
			}
			if effects.Load() != 1 || escaped == nil {
				t.Fatal("source tool callback effects", effects.Load())
			}
			entry, found, err := h.Entry(bg, committed)
			if err != nil || !found || entry.ByTask != escaped.TaskID() || entry.Kind != "test.note" {
				t.Error("tool commit lost adopted entry attribution", entry, found, err)
			}
			if values, ok := any(escaped).(committer); ok {
				called := false
				value, err := values.Commit(bg, func(*Tx) (any, error) { called = true; return "late", nil })
				if err == nil || value != nil || called {
					t.Error("ended tool retained callback commit authority", value, err, called)
				}
			}
		})
	}
}
