package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestNestedTool120BlockDiagnosticAndArbitraryDetailsFidelity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		reg := wrapRegistration("nested_blocks")
		reg.OutputLimits = &ToolOutputLimits{MaxBytes: 8, MaxLines: 10, Retain: "head"}
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{Blocks: []goai.ContentBlock{{Type: "text", Text: "abcdefghijkl"}, {Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}, Diagnostics: []ToolDiagnostic{{Severity: "warn", Message: "warning", Code: "test"}}, HasDetails: true, DetailsValue: []any{"array", nil}}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "nested-fidelity-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, e := r.CallTool(ctx, "blocks", "nested_blocks", JSON{})
			if e != nil {
				return e
			}
			if result.IsError || len(result.Blocks) != 2 || result.Blocks[0].Text != "abcdefgh" || result.Blocks[1].Data != "aGVsbG8=" || len(result.Diagnostics) != 1 || !result.HasDetails {
				t.Fatal("result fidelity", result)
			}
			details, ok := result.DetailsValue.([]any)
			if !ok || len(details) != 2 || details[1] != nil {
				t.Fatal("arbitrary details lost", result)
			}
			result.Blocks[1].Data = "mutated"
			again, e := r.CallTool(ctx, "blocks", "nested_blocks", JSON{})
			if e != nil {
				return e
			}
			if again.Blocks[1].Data != "aGVsbG8=" {
				t.Fatal("nested block alias")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
func TestNestedTool120DiagnosticJSONSeverityPreservesApplicationCommit(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		commits := 0
		registry := NewRegistry()
		reg := wrapRegistration("nested_bad_diagnostics")
		reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			task, found, err := api.Task(ctx, api.TaskID())
			if err != nil || !found {
				return ToolResult{}, errors.New("nested task missing")
			}
			if _, err := api.Commit(ctx, func(tx *Tx) (any, error) {
				commits++
				return tx.AppendEntry(task.Conversation, JSON{"kind": "probe.nested-diagnostic-effect", "data": JSON{"severity": "invalid"}})
			}); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: "text", Diagnostics: []ToolDiagnostic{{Severity: "invalid", Message: "bad"}}}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "nested-bad-diagnostics-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, e := r.CallTool(ctx, "bad", "nested_bad_diagnostics", JSON{})
			if e != nil {
				return e
			}
			if result.IsError || result.Content != "text" || !result.HasStructuredOutput || result.StructuredOutput != "text" || !reflect.DeepEqual(result.Diagnostics, []ToolDiagnostic{{Severity: "invalid", Message: "bad"}}) {
				t.Error("nested JSON diagnostic changed", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" || commits != 1 {
			t.Fatal(record, commits)
		}
	})
}
