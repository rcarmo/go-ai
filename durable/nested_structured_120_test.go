package durable

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNestedTool120StructuredSchemaPresenceValidationAndIsolation(t *testing.T) {
	for _, mode := range []string{"valid", "invalid", "missing", "null", "undeclared", "text"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var callbacks atomic.Int64
				reg := wrapRegistration("structured_nested")
				if mode == "valid" || mode == "invalid" || mode == "missing" {
					reg.StructuredOutputSchema = JSON{"type": "object", "properties": JSON{"n": JSON{"type": "integer"}}, "required": []any{"n"}, "additionalProperties": false}
				} else if mode == "null" {
					reg.StructuredOutputSchema = JSON{"type": "null"}
				}
				reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					result := ToolResult{Content: "bounded text", Commit: func(*Tx) error { callbacks.Add(1); return nil }}
					switch mode {
					case "valid":
						result.StructuredOutput = JSON{"n": 2}
					case "invalid":
						result.StructuredOutput = JSON{"n": "bad"}
					case "null":
						result.HasStructuredOutput = true
					case "undeclared":
						result.StructuredOutput = JSON{"n": 2}
					}
					return result, nil
				}
				if e := registry.Register(reg); e != nil {
					t.Fatal(e)
				}
				def := taskDefinition(t, "structured-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					result, e := r.CallTool(ctx, "key", "structured_nested", JSON{})
					if e != nil {
						return e
					}
					switch mode {
					case "valid":
						if !result.HasStructuredOutput || result.IsError {
							t.Fatal(result)
						}
						value, ok := result.StructuredOutput.(map[string]any)
						if !ok || value["n"] != json.Number("2") {
							t.Fatal(result)
						}
						value["n"] = 0
						again, e := r.CallTool(ctx, "key", "structured_nested", JSON{})
						if e != nil || again.StructuredOutput.(map[string]any)["n"] != json.Number("2") {
							t.Fatal("structured alias", again, e)
						}
					case "null":
						if !result.HasStructuredOutput || result.StructuredOutput != nil || result.IsError {
							t.Fatal(result)
						}
					case "invalid", "undeclared", "missing":
						if !result.IsError || result.HasStructuredOutput || len(result.Diagnostics) == 0 || result.Diagnostics[len(result.Diagnostics)-1].Code != "invalid_structured_output" {
							t.Fatal(result)
						}
					case "text":
						if result.StructuredOutput != "bounded text" || !result.HasStructuredOutput {
							t.Fatal(result)
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry})
				record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
				want := int64(1)
				if mode == "invalid" || mode == "undeclared" || mode == "missing" {
					want = 0
				}
				if record.State.Outcome.Status != "completed" || callbacks.Load() != want {
					t.Fatal(record, callbacks.Load())
				}
			})
		})
	}
}
func TestNestedTool120StructuredRegistrationRejectsUnsupportedAndCopiesSchema(t *testing.T) {
	r := NewRegistry()
	reg := wrapRegistration("structured_registration")
	schema := JSON{"type": "object", "properties": JSON{"n": JSON{"type": "integer"}}}
	reg.StructuredOutputSchema = schema
	if e := r.Register(reg); e != nil {
		t.Fatal(e)
	}
	schema["type"] = "string"
	stored, _ := r.current(reg.Definition.Name)
	if stored.offer.StructuredOutputSchema["type"] != "object" {
		t.Fatal("registration schema alias")
	}
	reg.StructuredOutputSchema = JSON{"type": "object", "oneOf": []any{JSON{"type": "string"}}}
	if e := r.Register(reg); e == nil || !strings.Contains(e.Error(), "unsupported") {
		t.Fatal("unsupported result schema accepted", e)
	}
}
