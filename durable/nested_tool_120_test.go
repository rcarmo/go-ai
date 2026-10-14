package durable

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
)

func TestNestedTool120KeyReuseResultOwnershipAndAuthority(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var runs atomic.Int64
		var escaped *NestedToolHandle
		var apiEscaped *ToolAPI
		reg := wrapRegistration("nested_work")
		reg.Callers = []string{"tools"}
		reg.Deferred = true
		reg.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"]}`)
		reg.Execute = func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
			runs.Add(1)
			apiEscaped = api
			if err := api.Output("prefix"); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Details: JSON{"value": args["value"]}}, nil
		}
		def := taskDefinition(t, "nested-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			first, e := r.StartTool(ctx, "stable", "nested_work", JSON{"value": 1})
			if e != nil {
				return e
			}
			escaped = first
			second, e := r.StartTool(ctx, "stable", "nested_work", JSON{"value": 1})
			if e != nil {
				return e
			}
			if first.ID() != second.ID() {
				t.Fatal("key created duplicate")
			}
			if _, e = r.StartTool(ctx, "stable", "nested_work", JSON{"value": 2}); e == nil {
				t.Fatal("changed arguments reused key")
			}
			result, e := first.Wait(ctx)
			if e != nil {
				return e
			}
			if result.Content != "prefix" || result.IsError || result.Details["value"] != json.Number("1") {
				t.Fatal(result)
			}
			result.Details["value"] = 0
			again, e := second.Wait(ctx)
			if e != nil {
				return e
			}
			if again.Details["value"] != json.Number("1") {
				t.Fatal("result alias")
			}
			if _, e = r.StartTool(ctx, "forbidden", ToolSearchName, JSON{"query": "work"}); e == nil {
				t.Fatal("model-only nested authority")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
		})
		r := NewRegistry()
		if e := r.Register(reg); e != nil {
			t.Fatal(e)
		}
		search, e := ToolSearchRegistration(1)
		if e != nil {
			t.Fatal(e)
		}
		if e = r.Register(search); e != nil {
			t.Fatal(e)
		}
		if _, e = r.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: r})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("parent outcome: %#v error:%#v", record.State.Outcome, record.State.Outcome.Error)
		}
		if runs.Load() != 1 {
			t.Fatal("duplicate nested effects", runs.Load())
		}
		if _, e = escaped.Wait(bg); !errors.Is(e, ErrSealed) {
			t.Fatal("escaped handle authority", e)
		}
		if e = apiEscaped.Output("late"); !errors.Is(e, ErrSealed) {
			t.Fatal("escaped API", e)
		}
		state, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, doc := range state.Documents {
			if doc.Kind == nestedCallDocument {
				found = true
				if !doc.Retired {
					t.Fatal("caller result did not retire")
				}
			}
		}
		if !found || len(state.Entries) != 0 {
			t.Fatal("nested call leaked transcript or missing document", len(state.Entries))
		}
	})
}
