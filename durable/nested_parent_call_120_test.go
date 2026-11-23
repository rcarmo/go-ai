package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestNestedTool120ParentCallAttribution(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		leaf := wrapRegistration("leaf")
		leaf.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "leaf"}, nil }
		middle := wrapRegistration("middle")
		middle.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			return api.CallTool(ctx, "inner", "leaf", JSON{})
		}
		outer := wrapRegistration("outer")
		outer.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			return api.CallTool(ctx, "middle-key", "middle", JSON{})
		}
		for _, reg := range []ToolRegistration{leaf, middle, outer} {
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
		}
		seen := map[string]ToolInvocation{}
		extension := &Extension{Name: "parent-attribution", ToolHooks: ToolHooks{BeforeTool: func(ctx context.Context, call goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
			identity, err := api.ToolInvocation(ctx)
			if err != nil {
				return nil, err
			}
			seen[call.Name] = identity
			return nil, nil
		}}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		options := Options{Registry: registry}
		def := taskDefinition(t, "parent-call-fixture", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.CallTool(ctx, "outer-key", "outer", JSON{}); err != nil {
				return err
			}
			snapshot, err := r.harness.Snapshot(ctx)
			if err != nil {
				return err
			}
			checked := false
			for _, task := range snapshot.Tasks {
				if task.Kind != nestedToolKind {
					continue
				}
				record, err := CanonicalTask(task, r.harness.session.limits)
				if err != nil {
					return err
				}
				input, err := nestedInput(record, r.harness.session.limits)
				if err != nil {
					return err
				}
				if input.Name != "leaf" {
					continue
				}
				input.ParentCallID = "forged"
				value, err := dtoObject(input, r.harness.session.limits)
				if err != nil {
					return err
				}
				task.Execution.Native.Input = &TaskValue{Present: true, Value: value}
				if err := validateNestedReferences(snapshot, task, r.harness.session.limits); err == nil {
					t.Error("forged parent attribution accepted")
				}
				checked = true
			}
			if !checked {
				t.Error("leaf task absent")
			}
			if seen["middle"].ParentCallID != "outer-key" || seen["leaf"].ParentCallID != "outer-key/middle-key" || seen["leaf"].ParentTask != seen["middle"].Task {
				t.Error("nested parent attribution", seen)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, options)
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}

func TestNestedTool120ModelParentCallAttribution(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		leaf := wrapRegistration("leaf")
		leaf.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "leaf"}, nil }
		outer := wrapRegistration("outer")
		outer.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			return api.CallTool(ctx, "child", "leaf", JSON{})
		}
		var identity ToolInvocation
		extension := &Extension{Name: "model-parent", Tools: []ToolRegistration{leaf, outer}, ToolHooks: ToolHooks{BeforeTool: func(ctx context.Context, call goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
			if call.Name == "leaf" {
				var err error
				identity, err = api.ToolInvocation(ctx)
				return nil, err
			}
			return nil, nil
		}}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		requests := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			requests++
			ch := make(chan goai.Event, 1)
			if requests == 1 {
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "model-call-120", Name: "outer", Arguments: map[string]any{}}}}}
			} else {
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "run"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sub.Wait(bg); err != nil {
			t.Fatal(err)
		}
		if identity.ParentCallID != "model-call-120" || identity.CallID != "model-call-120/child" || identity.Key != "child" || identity.Caller != "tools" || identity.Task == 0 || identity.ParentTask == 0 {
			t.Fatal("model parent attribution lost", identity)
		}
		record, found, err := h.Task(bg, identity.Task)
		wantInput := JSON{"kind": "nested", "parent": float64(identity.ParentTask), "parentCallId": "model-call-120", "key": "child", "call": JSON{"type": "toolCall", "id": "model-call-120/child", "name": "leaf", "arguments": JSON{}}}
		// The pinned pi.tool is version 4 (tool.ts:157); the new parked
		// checkpoint arm did not change this owned nested input.
		if err != nil || !found || record.Owner != identity.ParentTask || record.Version != 4 || record.Input == nil || !record.Input.Present || !equalJSONValue(record.Input.Value, wantInput) {
			t.Fatal("canonical nested input and ownership", record, record.Input, wantInput, err)
		}
	})
}

func openNestedResultHarness(t *testing.T, store Storage, tools ...ToolRegistration) (*Harness, *ConversationHandle) {
	t.Helper()
	registry := NewRegistry()
	for _, tool := range tools {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls++
		ch := make(chan goai.Event, 1)
		if calls == 1 {
			ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "c1", Name: "batch", Arguments: JSON{}}}}}
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	return h, root(t, h, ref)
}

// harness-nested-tools.test.ts:175-217. Observe the automatic keyed call
// while the caller still owns its result and again after it retires.
func TestSourceV160NestedToolOwnsResultOutsideTranscript(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		type witness struct {
			parent, child ID
			result        ToolResult
			stored        JSON
		}
		seen := make(chan witness, 1)
		key := "1"
		address := func(parent ID) DocumentAddress {
			return DocumentAddress{Scope: "task", Kind: "pi.tool.nested-call", Owner: parent, Key: key, Family: true}
		}
		echo := wrapRegistration("echo")
		echo.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`)
		echo.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
			text, _ := generationObject(args)["text"].(string)
			return ToolResult{Content: "echo " + text}, nil
		}
		batch := wrapRegistration("batch")
		batch.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			child, err := api.StartToolAuto(ctx, "echo", JSON{"text": "a"})
			if err != nil {
				return ToolResult{}, err
			}
			result, err := child.Wait(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			doc, found, err := b.store.FindDocument(ctx, address(api.TaskID()), CurrentDocumentPoint())
			if err != nil || !found {
				return ToolResult{}, fmt.Errorf("nested result lookup: found=%t: %v", found, err)
			}
			doc, found, err = b.store.DocumentAt(ctx, doc.ID, CurrentDocumentPoint())
			if err != nil || !found {
				return ToolResult{}, fmt.Errorf("nested result read: found=%t: %v", found, err)
			}
			seen <- witness{api.TaskID(), child.ID(), result, generationObject(doc.Value["result"])}
			return ToolResult{Content: fmt.Sprint("got ", result.StructuredOutput)}, nil
		}
		h, conversation := openNestedResultHarness(t, b.store, echo, batch)
		submission, err := conversation.Submit(bg, Input{Type: "input", Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if settled := waitSubmission(t, submission); settled.Record.Status != "done" {
			t.Fatal("submission", settled.Record)
		}
		got := watchReceive(t, seen)
		if got.child == 0 || got.result.IsError || !got.result.HasStructuredOutput || got.result.StructuredOutput != "echo a" || len(got.result.Diagnostics) != 0 {
			t.Fatal("nested result delivered to caller", got)
		}
		duration, number := exactNumber(got.stored["durationMs"])
		wantResult := JSON{"taskId": float64(got.child), "structuredOutput": "echo a", "isError": false, "diagnostics": []any{}, "durationMs": got.stored["durationMs"]}
		if !number || duration.Sign() < 0 || !equalJSONValue(got.stored, wantResult) {
			t.Fatal("stored nested result", got.stored)
		}
		if got.result.DurationMs == nil || !duration.IsInt() || !duration.Num().IsInt64() || *got.result.DurationMs != duration.Num().Int64() {
			t.Fatalf("nested Wait omitted or changed the stored duration %v: %+v", got.stored["durationMs"], got.result)
		}
		page, err := conversation.Entries(bg, EntryQuery{Order: Ascending}, 10, nil)
		if err != nil || page.Next != nil {
			t.Fatal(page, err)
		}
		var results []MessageReceipt
		for _, entry := range page.Items {
			for _, receipt := range entry.Model {
				if receipt.Role == goai.RoleToolResult {
					results = append(results, receipt)
				}
			}
		}
		if len(results) != 1 || results[0].ToolCallID != "c1" || v160ToolResultText(results[0]) != "got echo a" {
			t.Fatal("model transcript tool results", results)
		}
		encoded, err := json.Marshal(results[0])
		if err != nil {
			t.Fatal(err)
		}
		var wire JSON
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		if _, nested := wire["nestedCalls"]; nested {
			t.Fatal("nested results leaked into model receipt", wire)
		}
		parent, found, err := h.Task(bg, got.parent)
		if err != nil || !found || parent.Input == nil || generationObject(parent.Input.Value)["kind"] != "model" || generationObject(parent.Input.Value)["callId"] != "c1" {
			t.Fatal("model-issued task", parent, found, err)
		}
		child, found, err := h.Task(bg, got.child)
		if err != nil || !found || child.Owner != parent.ID || child.Input == nil || !child.Input.Present {
			t.Fatal("nested task ownership", child, found, err)
		}
		wantInput := JSON{"kind": "nested", "parent": float64(parent.ID), "parentCallId": "c1", "key": "1",
			"call": JSON{"type": "toolCall", "id": "c1/1", "name": "echo", "arguments": JSON{"text": "a"}}}
		if !equalJSONValue(child.Input.Value, wantInput) {
			t.Fatal("nested task input", child.Input.Value, wantInput)
		}
		encoded, err = json.Marshal(child.State)
		if err != nil {
			t.Fatal(err)
		}
		var state JSON
		if err := json.Unmarshal(encoded, &state); err != nil {
			t.Fatal(err)
		}
		wantState := JSON{"status": "terminal", "outcome": JSON{"status": "completed", "result": JSON{"kind": "nested"}}}
		if !equalJSONValue(state, wantState) {
			t.Fatal("nested terminal receipt", state, wantState)
		}
		if doc, found, err := b.store.FindDocument(bg, address(parent.ID), CurrentDocumentPoint()); err != nil || found {
			t.Fatal("nested result was not retired", doc, found, err)
		}
		if live, found, err := conversation.SnapshotDefinition(bg, LiveDoc(), nil); err != nil || !found || !equalJSONValue(live.Copy(), JSON{}) {
			t.Fatal("run left a live tool slot", live, found, err)
		}
	})
}

// harness-nested-tools.test.ts:1096-1122. The unsupported details value
// fails settlement, so the fallback result has diagnostics but no duration.
func TestSourceV160FaultedNestedToolOmitsDuration(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		type witness struct {
			id     ID
			result ToolResult
		}
		seen := make(chan witness, 1)
		echo := wrapRegistration("echo")
		echo.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
			return ToolResult{DetailsValue: JSON{"count": make(chan int)}, HasDetails: true}, nil
		}
		batch := wrapRegistration("batch")
		batch.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			child, err := api.StartToolAuto(ctx, "echo", JSON{"text": "a"})
			if err != nil {
				return ToolResult{}, err
			}
			got, err := child.Wait(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			seen <- witness{child.ID(), got}
			return ToolResult{}, nil
		}
		h, conversation := openNestedResultHarness(t, b.store, echo, batch)
		submission, err := conversation.Submit(bg, Input{Type: "input", Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, submission)
		got := watchReceive(t, seen)
		child, found, err := h.Task(bg, got.id)
		if err != nil || !found || child.ID != got.id || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "faulted" {
			t.Fatal("faulted nested task", child, found, err)
		}
		result := got.result
		if !result.IsError || result.DurationMs != nil || result.HasStructuredOutput || result.HasDetails || result.Usage != nil || result.Content != "" || len(result.Blocks) != 0 || len(result.Diagnostics) != 1 {
			t.Fatalf("faulted nested result; want diagnostics only: %+v", result)
		}
		diagnostic := result.Diagnostics[0]
		if diagnostic.Severity != "error" || diagnostic.Code != "faulted" || !strings.Contains(diagnostic.Message, "Tool echo failed") {
			t.Fatal("faulted nested diagnostic", diagnostic)
		}
	})
}
