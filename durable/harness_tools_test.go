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
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func registerSum(t *testing.T, registry *Registry, effects *atomic.Int64, document ID) {
	t.Helper()
	e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "sum", Description: "Add two bounded integers", Parameters: json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer","minimum":0,"maximum":100},"b":{"type":"integer","minimum":0,"maximum":100}},"required":["a","b"],"additionalProperties":false}`)}, Implementation: "sum.native", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, rawToolArguments any, api *ToolAPI) (ToolResult, error) {
		args := rawToolArguments.(JSON)
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
	assertToolUsageReceipts(t, h, snapshot, "sum", &goai.Usage{Output: 1, TotalTokens: 1}, h.session.limits)
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
	assertToolUsageReceipts(t, h2, reopened, "sum", &goai.Usage{Output: 1, TotalTokens: 1}, h2.session.limits)
}

func TestM1cSourceRepairBeforeIntent(t *testing.T) {
	var effects, repairs atomic.Int64
	var executeRecord TaskRecord
	registry := NewRegistry()
	e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "repair", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","minimum":1,"maximum":9}},"required":["n"],"additionalProperties":false}`)}, Implementation: "repair.impl", Version: 1, ReplaySafe: true, PrepareArguments: func(context.Context, any) (any, error) { repairs.Add(1); return JSON{"n": 3}, nil }, Execute: func(ctx context.Context, rawToolArguments any, api *ToolAPI) (ToolResult, error) {
		args := rawToolArguments.(JSON)
		effects.Add(1)
		if args["n"] != json.Number("3") {
			t.Error("rewritten args not stored")
		}
		record, found, e := api.Task(ctx, api.TaskID())
		if e != nil || !found || taskCheckpointMap(record.State.Checkpoint)["phase"] != "execute" || !equalJSONValue(taskCheckpointMap(taskCheckpointMap(record.State.Checkpoint)["arguments"]), JSON{"n": 3}) {
			t.Error("effect before final source intent", record, e)
		}
		executeRecord = record
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
	conversation := root(t, h, ref)
	sub, e := conversation.Submit(bg, Input{Content: "repair"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, sub).Submission.Status != "done" || effects.Load() != 1 || repairs.Load() != 1 {
		t.Fatal("repair/effectcount")
	}
	if !equalJSONValue(taskCheckpointMap(taskCheckpointMap(executeRecord.State.Checkpoint)["arguments"]), JSON{"n": 3}) {
		t.Fatal("actual execute checkpoint changed", executeRecord)
	}
	view, e := conversation.ContextView(bg, 0)
	if e != nil {
		t.Fatal(e)
	}
	original := 0
	for _, message := range view.Messages {
		for _, block := range message.Content {
			if block.Type == "toolCall" && block.Name == "repair" {
				original++
				if !equalJSONValue(block.Arguments, JSON{"n": 2}) {
					t.Fatal("stored original call changed", block)
				}
			}
		}
	}
	if original != 1 {
		t.Fatal("stored original call count", original)
	}
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
						args["a"] = JSON{"not": "an integer"}
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
			children := 0
			for _, task := range state.Tasks {
				if task.Kind != "pi.tool" {
					continue
				}
				children++
				record, found, err := h.Task(bg, task.ID)
				if err != nil || !found || record.State.Outcome == nil || record.State.Outcome.Result == nil || record.Input == nil {
					t.Fatal("missing public tool outcome", record, err)
				}
				input := generationObject(record.Input.Value)
				if input["callId"] != "call" {
					t.Fatal("tool call ownership", input)
				}
				if mode == "not-offered" {
					if input["unoffered"] != true {
						t.Fatal("unoffered task not flagged", input)
					}
				} else if _, exists := input["unoffered"]; exists {
					t.Fatal("offered task acquired unoffered field", input)
				}
				result := generationObject(record.State.Outcome.Result.Value)
				entry, found := state.Entries[submissionEntryID(result["entryId"])]
				if result["kind"] != "model" || !found || entry.Kind != "pi.tool-result" || len(entry.Model) != 1 || !entry.Model[0].IsError || entry.Model[0].ToolCallID != "call" {
					t.Fatal("missing explicit tool error", result, entry)
				}
				code := "invalid_arguments"
				if mode == "not-offered" {
					code = "tool_unavailable"
				}
				diagnostics, valid := generationObject(entry.Data)["diagnostics"].([]any)
				if !valid || len(diagnostics) != 1 || generationObject(diagnostics[0])["code"] != code {
					t.Fatal("tool error diagnostic", entry.Data)
				}
			}
			if children != 1 {
				t.Fatal("expected one own error task", children)
			}
		})
	}
}

// Source generation.ts tools/abort and scheduler.abortConversation retain the
// waiting owner until its ordinary child ends. Late returned usage is unadopted.
func TestM1cAbortDrainsChildBeforeParentAndFencesEscapedAPI(t *testing.T) {
	var effects, appEffects, requests atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	var escaped *ToolAPI
	registry := NewRegistry()
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "block", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "block.impl", Version: 1, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		escaped = api
		if err := api.Output("retained prefix"); err != nil {
			return ToolResult{}, err
		}
		if err := api.Details(JSON{"retained": true}); err != nil {
			return ToolResult{}, err
		}
		close(entered)
		<-release
		if _, err := api.Commit(ctx, func(tx *Tx) (any, error) {
			appEffects.Add(1)
			record, found, err := tx.Task(api.TaskID())
			if err != nil || !found {
				return nil, fmt.Errorf("own task absent: %w", err)
			}
			return tx.AppendEntry(record.Conversation, JSON{"kind": "test.late-abort-effect"})
		}); err == nil {
			t.Error("aborted public application commit succeeded")
		}
		return ToolResult{Content: "ignored afterabort", Usage: &goai.Usage{Input: 1, TotalTokens: 1}}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		requests.Add(1)
		events := make(chan goai.Event, 1)
		events <- toolAnswer("call", "block", JSON{})
		close(events)
		return events
	})
	options.Registry = registry
	store, dir := newSQLiteTestStore(t)
	h := openHarness(t, store, options)
	cleanupTaskGates(t, release)
	conversation := root(t, h, ref)
	var mu sync.Mutex
	marked := make(chan struct{}, 1)
	marks := map[string]bool{}
	terminal := map[string]uint64{}
	var parentID ID
	observer, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, frame CommitPublication) error {
		mu.Lock()
		defer mu.Unlock()
		for index := range frame.Changes.Len() {
			node, _ := frame.Changes.Index(index)
			change := generationObject(node.Copy())
			if change["type"] != "task" {
				continue
			}
			record := generationObject(change["value"])
			kind, _ := record["kind"].(string)
			if kind != "pi.generation" && kind != "pi.tool" {
				continue
			}
			if kind == "pi.generation" {
				parentID = submissionEntryID(record["id"])
			}
			if record["abortRequested"] == true {
				marks[kind] = true
			}
			if generationObject(record["state"])["status"] == "terminal" {
				terminal[kind] = frame.Seq
			}
		}
		if len(marks) == 2 {
			select {
			case marked <- struct{}{}:
			default:
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Detach()
	submission, err := conversation.Submit(bg, Input{Content: "block"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	aborted := make(chan error, 1)
	go func() { aborted <- conversation.Abort(ctx) }()
	watchReceive(t, marked)
	cancel()
	if err := watchReceive(t, aborted); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation must stop only its own wait", err)
	}
	mu.Lock()
	parent := parentID
	mu.Unlock()
	generation, found, err := h.Task(bg, parent)
	if err != nil || !found || !generation.AbortRequested || generation.State.Status != "waiting" || taskCheckpointMap(generation.State.Checkpoint)["phase"] != "tools" {
		t.Fatal("owner must remain waiting before child drain", generation, err)
	}
	child, found, err := h.Task(bg, escaped.TaskID())
	if err != nil || !found || !child.AbortRequested || child.State.Status == "terminal" {
		t.Fatal("noncooperative child lost ownership", child, err)
	}
	releaseTaskGate(release)
	settled := waitSubmission(t, submission)
	if settled.Record.Status != "unanswered" || settled.Record.Reason != "aborted" || effects.Load() != 1 || appEffects.Load() != 0 || requests.Load() != 1 {
		t.Fatal("abort settlement/effects", settled.Record, effects.Load(), appEffects.Load(), requests.Load())
	}
	// Supplemental native capability error; the source guarantee checked below
	// is that no late commit or returned usage reaches the public record.
	if err := escaped.Output("postreturn"); !errors.Is(err, ErrSealed) {
		t.Fatal(err)
	}
	mu.Lock()
	childSeq, parentSeq := terminal["pi.tool"], terminal["pi.generation"]
	mu.Unlock()
	if childSeq == 0 || parentSeq <= childSeq {
		t.Fatal("terminal adoption must be child before owner", childSeq, parentSeq)
	}
	check := func(h *Harness) {
		t.Helper()
		child, found, err := h.Task(bg, escaped.TaskID())
		if err != nil || !found || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "aborted" || child.State.Outcome.Result == nil {
			t.Fatal("own aborted tool", child, err)
		}
		result := generationObject(child.State.Outcome.Result.Value)
		entry, found, err := h.Entry(bg, submissionEntryID(result["entryId"]))
		if err != nil || !found || len(result) != 2 || result["kind"] != "model" || entry.Kind != "pi.tool-result" || entry.ByTask != child.ID || len(entry.Model) != 1 {
			t.Fatal("own aborted result entry", result, entry, err)
		}
		message := entry.Model[0]
		if !message.IsError || message.ToolCallID != "call" || message.ToolName != "block" || message.Usage != nil || !message.HasDetails || !equalJSONValue(message.DetailsValue, JSON{"retained": true}) || len(message.Content) != 2 || message.Content[0].Type != "text" || message.Content[0].Text != "retained prefix" || message.Content[1].Type != "text" || message.Content[1].Text != "<harness>\n[error] Tool block was aborted\n</harness>" {
			t.Fatal("aborted retained result", message)
		}
		if !equalJSONValue(generationObject(entry.Data)["diagnostics"], []any{JSON{"severity": "error", "code": "aborted", "message": "Tool block was aborted"}}) {
			t.Fatal("aborted result diagnostics", entry.Data)
		}
		generation, found, err := h.Task(bg, parent)
		if err != nil || !found || generation.State.Status != "terminal" || generation.State.Outcome == nil || generation.State.Outcome.Status != "aborted" || generation.State.Outcome.Result != nil {
			t.Fatal("own aborted generation", generation, err)
		}
		handle, err := h.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		view, err := handle.View(bg)
		if err != nil {
			t.Fatal(err)
		}
		usage, ok := view.Docs.Get("pi.usage")
		if !ok {
			t.Fatal("usage absent")
		}
		tools, ok := usage.Get("tools")
		if !ok {
			t.Fatal("tool ledger absent")
		}
		if _, found := tools.Get("block"); found {
			t.Fatal("uncommitted returned usage billed", usage.Copy())
		}
		toolResults := 0
		for index := range view.Entries.Len() {
			entry, _ := view.Entries.Index(index)
			kind, _ := entry.Get("kind")
			if kind.String() == "test.late-abort-effect" {
				t.Error("late app effect committed", entry.Copy())
			}
			if kind.String() == "pi.tool-result" {
				toolResults++
			}
		}
		if toolResults != 1 {
			t.Fatal("abort duplicated result", toolResults)
		}
	}
	check(h)
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h2 := openHarness(t, store, options)
	resumed, err := h2.Submission(bg, submission.ID())
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, resumed); result.Record.Status != "unanswered" || result.Record.Reason != "aborted" || effects.Load() != 1 {
		t.Fatal("reopened abort changed or replayed", result.Record, effects.Load())
	}
	check(h2)
}

func TestM1cCloseNoncooperativeToolKeepsOwnershipAndRecoveryOutcome(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var escaped *ToolAPI
	var effects atomic.Int64
	registry := NewRegistry()
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "block", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "block.impl", Version: 1, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		escaped = api
		close(entered)
		<-release
		return ToolResult{Content: "finished"}, nil
	}}); e != nil {
		t.Fatal(e)
	}
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("call", "block", JSON{})
		} else {
			results := 0
			for _, message := range input.Messages {
				if message.Role == goai.RoleToolResult {
					results++
					if !message.IsError || message.ToolCallID != "call" || goai.ContentText(message.Content) != "<harness>\n[error] Tool block was interrupted and may have partially run\n</harness>" {
						t.Error("unsafe interrupted result", message)
					}
				}
			}
			if results != 1 {
				t.Error("missing recovery result", results)
			}
			ch <- terminal("answer")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	s, dir := newSQLiteTestStore(t)
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	cleanupTaskGates(t, release)
	sub, e := r.Submit(bg, Input{Content: "close"})
	if e != nil {
		t.Fatal(e)
	}
	awaitTaskSignal(t, entered)
	toolID := escaped.TaskID()
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
	recovered, found, e := h2.Task(bg, toolID)
	if e != nil || !found || recovered.Version != 3 || taskCheckpointMap(recovered.State.Checkpoint)["phase"] != "execute" || taskCheckpointMap(recovered.State.Checkpoint)["replay"] != "unsafe" {
		t.Fatal("unsafe execute intent lost on close", recovered, e)
	}
	resumed, e := h2.Submission(bg, sub.ID())
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, resumed).Record.Status != "done" || requests.Load() != 2 || effects.Load() != 1 {
		t.Fatal("tool replay orlostsuccessor")
	}
	settled, found, e := h2.Task(bg, toolID)
	if e != nil || !found || settled.State.Outcome == nil || settled.State.Outcome.Status != "failed" || settled.State.Outcome.Result == nil || generationObject(settled.State.Outcome.Result.Value)["kind"] != "model" {
		t.Fatal("unsafe tool recovery outcome", settled, e)
	}
}

// Selected source tool.ts current phase resolution/output/ToolAPI.commit rules.
// A failed public commit rolls back before its error becomes the tool result.
func TestM1cRegistryPhasePinOutputCapAndAtomicAppFailure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var oldCalls, newCalls atomic.Int64
	registry := NewRegistry()
	failedCommit := func(ctx context.Context, api *ToolAPI) (ToolResult, error) {
		if err := api.Output(strings.Repeat("x", MaxToolOutputBytes+1)); err != nil {
			return ToolResult{}, err
		}
		_, err := api.Commit(ctx, func(tx *Tx) (any, error) {
			task, found, err := tx.Task(api.TaskID())
			if err != nil || !found {
				return nil, fmt.Errorf("own task absent: %w", err)
			}
			if _, err := tx.AppendEntry(task.Conversation, JSON{"kind": "test.must-rollback"}); err != nil {
				return nil, err
			}
			return nil, errors.New("applicationfailure")
		})
		return ToolResult{}, err
	}
	reg := ToolRegistration{Definition: goai.Tool{Name: "pin", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "pin.impl", Version: 1, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		oldCalls.Add(1)
		return failedCommit(ctx, api)
	}}
	if err := registry.Install(&Extension{Name: "phase-pin", Tools: []ToolRegistration{reg}}); err != nil {
		t.Fatal(err)
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
	conversation := root(t, h, ref)
	cleanupTaskGates(t, release)
	sub, err := conversation.Submit(bg, Input{Content: "phasepin"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	reg.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		newCalls.Add(1)
		return failedCommit(ctx, api)
	}
	if err := registry.Install(&Extension{Name: "phase-pin", Tools: []ToolRegistration{reg}}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if settled := waitSubmission(t, sub); settled.Record.Status != "done" || oldCalls.Load() != 0 || newCalls.Load() != 1 || requests.Load() != 2 {
		t.Fatal("tool did not resolve current phase or useful answer", settled, oldCalls.Load(), newCalls.Load(), requests.Load())
	}
	value, err := h.Commit(bg, func(tx *Tx) (any, error) {
		return tx.ScanTasks(TaskQuery{Conversation: conversation.ID(), Kind: "pi.tool"}, 20, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	tasks := value.(Page[TaskRecord]).Items
	if len(tasks) != 1 {
		t.Fatal("expected own public tool", tasks)
	}
	task := tasks[0]
	outcome := task.State.Outcome
	if task.State.Status != "terminal" || outcome == nil || outcome.Status != "failed" || outcome.Error == nil || outcome.Error.Message != "Tool pin threw" || outcome.Result == nil {
		t.Fatal("throwing public commit outcome", task)
	}
	result := generationObject(outcome.Result.Value)
	entry, found, err := h.Entry(bg, submissionEntryID(result["entryId"]))
	if err != nil || !found || len(result) != 2 || result["kind"] != "model" || entry.Kind != "pi.tool-result" || entry.ByTask != task.ID || len(entry.Model) != 1 {
		t.Fatal("own tool result entry", result, entry, err)
	}
	message := entry.Model[0]
	wantDiagnostic := "<harness>\n[error] applicationfailure\n[warn] Output truncated to its beginning: 0 lines, 1 bytes dropped\n</harness>"
	if message.ToolCallID != "call" || message.ToolName != "pin" || !message.IsError || len(message.Content) != 2 || message.Content[0].Type != "text" || message.Content[0].Text != strings.Repeat("x", MaxToolOutputBytes) || message.Content[1].Type != "text" || message.Content[1].Text != wantDiagnostic {
		t.Fatal("bounded throwing tool result", len(message.Content), goai.ContentText(message.Content))
	}
	want := []any{JSON{"severity": "error", "code": "tool_error", "message": "applicationfailure"}, JSON{"severity": "warn", "code": "truncated", "message": "Output truncated to its beginning: 0 lines, 1 bytes dropped"}}
	if !equalJSONValue(generationObject(entry.Data)["diagnostics"], want) {
		t.Fatal("own ordered result diagnostics", entry.Data)
	}
	entries, err := conversation.Entries(bg, EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := 0
	for _, entry := range entries.Items {
		if entry.Kind == "test.must-rollback" {
			t.Error("throwing tool commit leaked application entry", entry)
		}
		if entry.Kind == "pi.tool-result" {
			results++
		}
	}
	if results != 1 {
		t.Fatal("duplicate tool result after rollback", results)
	}
}

func TestM1cCommittedPartialAndQueuedAbort(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	committed := make(chan struct{})
	var acknowledge, releaseOnce sync.Once
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event)
		go func() {
			defer close(ch)
			m := terminal("bounded prefix").Message
			select {
			case ch <- &goai.TextDeltaEvent{Partial: m, Delta: "bounded prefix"}:
			case <-ctx.Done():
				return
			}
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			select {
			case ch <- terminal("answer"):
			case <-ctx.Done():
			}
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
	zero, within := int64(0), float64(0)
	options.Settings = &HarnessSettings{Progress: &ProgressSettings{PartialIntervalMs: &zero, PersistWithinMs: &within}}
	h := openHarness(t, s, options)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	r := root(t, h, ref)
	first, e := r.Submit(bg, Input{Content: "active", RequestID: "active"})
	if e != nil {
		t.Fatal(e)
	}
	awaitTaskSignal(t, entered)
	awaitTaskSignal(t, committed)
	queued, e := r.Submit(bg, Input{Content: "withdraw", RequestID: "withdraw"})
	if e != nil {
		t.Fatal(e)
	}
	if result, err := queued.Abort(bg); err != nil || result != "aborted" {
		t.Fatal("queued abort", result, err)
	}
	if result, err := first.Abort(bg); err != nil || result != "already_placed" {
		t.Fatal("active input must remain placed", result, err)
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	partial := generationObject(generationObject(mainAbortFailureLive(t, state, r.ID())["generation"])["message"])
	wantContent := []any{JSON{"type": "text", "text": "bounded prefix"}}
	if partial["role"] != "assistant" || !equalJSONValue(partial["content"], wantContent) {
		t.Fatal("canonical partial not committed", partial)
	}
	status, err := queued.Status(bg)
	if err != nil || status.Status != "unanswered" || status.Reason != "aborted" || status.Entry != 0 || status.Answer != 0 {
		t.Fatal("queued abort not durable", status, err)
	}
	releaseOnce.Do(func() { close(release) })
	if waitSubmission(t, first).Submission.Status != "done" || waitSubmission(t, queued).Record.Reason != "aborted" || calls.Load() != 1 {
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
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "host", Parameters: json.RawMessage(`{"type":"object","oneOf":[{"required":["x"]}]}`)}, Implementation: "host.impl", Version: 1, Validator: func(context.Context, any) (any, error) { calls++; return nil, errors.New("strict host reject") }, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
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

// Native exact-number checks below are supplemental, not source numeric parity.
// Source registration accepts schema declarations; evaluation belongs at use.
func TestM1cNativeExactNumericProbeAndSourceDeclarationTiming(t *testing.T) {
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
	if e = malformed.Register(ToolRegistration{Definition: goai.Tool{Name: "malformed", Parameters: json.RawMessage(`{"type":"object","required":true,"unknown":true}`)}, Implementation: "malformed.impl", Version: 1, Validator: func(context.Context, any) (any, error) { return JSON{}, nil }, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); e != nil {
		t.Fatal("source declaration rejected before use", e)
	}
}

func TestM1cInvalidToolUsageSettlesOnceNoAppOrReplay(t *testing.T) {
	for _, mode := range []string{"negative-input", "negative-cost", "nan-cost", "inf-cost", "overflow", "valid-error"} {
		t.Run(mode, func(t *testing.T) {
			var effects, apps, requests atomic.Int64
			registry := NewRegistry()
			if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "usage", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "usage.impl", Version: 1, ReplaySafe: true, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
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
			wantApps := int64(0)
			if mode == "valid-error" {
				wantApps = 1
			}
			if waitSubmission(t, sub).Submission.Status != "done" || effects.Load() != 1 || apps.Load() != wantApps {
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
						record, err := CanonicalTask(task, h.session.limits)
						if err != nil || record.State.Outcome.Status != "failed" || record.State.Outcome.Error == nil || record.State.Outcome.Result == nil || !record.State.Outcome.Result.Present {
							t.Fatal("executor throw result", task, record, err)
						}
					} else {
						record, err := CanonicalTask(task, h.session.limits)
						want := "durable: rejected: invalid contribution usage"
						if mode == "nan-cost" || mode == "inf-cost" {
							want = "durable: rejected: Value contains a non-finite number and is not strict JSON"
						}
						if err != nil || record.State.Outcome.Status != "faulted" || record.State.Outcome.Error == nil || record.State.Outcome.Error.Message != want || cp.Result != nil {
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
			assertValidReceipt := func(state Snapshot) {
				t.Helper()
				matches := 0
				for _, entry := range state.Entries {
					if entry.Kind != "pi.tool-result" || len(entry.Model) != 1 || entry.Model[0].ToolName != "usage" {
						continue
					}
					matches++
					if !entry.Model[0].IsError || !reflect.DeepEqual(entry.Model[0].Usage, expectedUsage) {
						t.Fatal("paired executor error receipt changed", entry.Model[0])
					}
				}
				if matches != 1 {
					t.Fatalf("paired executor error receipts=%d; want 1", matches)
				}
			}
			if mode == "valid-error" {
				assertValidReceipt(state)
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
				assertValidReceipt(state)
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
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "repeat", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "repeat.impl", Version: 1, ReplaySafe: true, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
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

// Source scheduler.abortConversation joins ownership; a late response is neither
// an answer nor an offered tool execution. Marks are observed at public adoption.
func TestM1cAbortAtOfferedCallBoundaryStartsNoTool(t *testing.T) {
	var effects atomic.Int64
	registry := NewRegistry()
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "effect", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "effect.impl", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) { effects.Add(1); return ToolResult{}, nil }}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event)
		go func() { close(entered); <-release; ch <- toolAnswer("call", "effect", JSON{}); close(ch) }()
		return ch
	})
	options.Registry = registry
	store, _ := newTestSQLite(t)
	h := openHarness(t, store, options)
	cleanupTaskGates(t, release)
	conversation := root(t, h, ref)
	marked := make(chan ID, 1)
	observer, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, frame CommitPublication) error {
		for index := range frame.Changes.Len() {
			node, _ := frame.Changes.Index(index)
			change := generationObject(node.Copy())
			if change["type"] != "task" {
				continue
			}
			record := generationObject(change["value"])
			if record["kind"] == "pi.generation" && record["abortRequested"] == true {
				select {
				case marked <- submissionEntryID(record["id"]):
				default:
				}
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Detach()
	submission, err := conversation.Submit(bg, Input{Content: "abortboundary"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	aborted := make(chan error, 1)
	go func() { aborted <- conversation.Abort(ctx) }()
	taskID := watchReceive(t, marked)
	task, found, err := h.Task(bg, taskID)
	if err != nil || !found || !task.AbortRequested || task.State.Status == "terminal" {
		t.Fatal("abort mark/host ownership", task, err)
	}
	cancel()
	if err := watchReceive(t, aborted); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation must stop only its own wait", err)
	}
	task, found, err = h.Task(bg, taskID)
	if err != nil || !found || task.State.Status == "terminal" {
		t.Fatal("caller cancellation detached the provider", task, err)
	}
	releaseTaskGate(release)
	settled := waitSubmission(t, submission)
	if settled.Record.Status != "unanswered" || settled.Record.Reason != "aborted" || settled.Record.Answer != 0 || effects.Load() != 0 {
		t.Fatal("late response dispatched or answered", settled.Record, effects.Load())
	}
	task, found, err = h.Task(bg, taskID)
	if err != nil || !found || task.State.Status != "terminal" || task.State.Outcome == nil || task.State.Outcome.Status != "aborted" || task.State.Outcome.Result != nil {
		t.Fatal("own generation abort outcome", task, err)
	}
	value, err := h.Commit(bg, func(tx *Tx) (any, error) {
		return tx.ScanTasks(TaskQuery{Conversation: conversation.ID(), Kind: "pi.tool"}, 20, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if tasks := value.(Page[TaskRecord]).Items; len(tasks) != 0 {
		t.Fatal("late call started a tool task", tasks)
	}
	view, err := conversation.View(bg)
	if err != nil {
		t.Fatal(err)
	}
	for index := range view.Entries.Len() {
		entry, _ := view.Entries.Index(index)
		kind, _ := entry.Get("kind")
		if kind.String() == "pi.assistant" || kind.String() == "pi.tool-result" {
			t.Error("uncommitted abort response published", entry.Copy())
		}
	}
	usage, found := view.Docs.Get("pi.usage")
	if !found {
		t.Fatal("usage doc absent")
	}
	models, found := usage.Get("models")
	if !found || models.Len() != 0 {
		t.Fatal("uncommitted abort response billed", usage.Copy())
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
func assertToolUsageReceipts(t *testing.T, h *Harness, state Snapshot, name string, want *goai.Usage, l Limits) {
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
		public, found, err := h.Task(bg, task.ID)
		if err != nil || !found || public.Version != 3 || public.State.Outcome == nil || public.State.Outcome.Result == nil {
			t.Fatal("public tool outcome absent", public, err)
		}
		result := generationObject(public.State.Outcome.Result.Value)
		if result["kind"] != "model" {
			t.Fatal("expected model-issued tool result", result)
		}
		entry, found := state.Entries[submissionEntryID(result["entryId"])]
		if !found || entry.Kind != "pi.tool-result" || len(entry.Model) != 1 {
			t.Fatal("tool outcome must identify its actual result entry", result, entry)
		}
		receipt := entry.Model[0]
		if receipt.ToolName != name {
			continue
		}
		children++
		if public.Input == nil {
			t.Fatal("tool input absent", public)
		}
		input := generationObject(public.Input.Value)
		if input["kind"] != "model" || input["callId"] != receipt.ToolCallID || entry.ByTask != public.ID {
			t.Fatal("tool receipt ownership changed", input, entry, public)
		}
		matches(receipt.Usage)
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
			reg.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
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
