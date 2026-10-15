package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

// Pinned durable/src/codemode/codemode.ts outcomeOf: defined structured output
// resolves regardless of IsError; otherwise failure text falls back to diagnostics.
func TestCodemode120ToolErrorOutcomeAndSettledReplay(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	cases := []struct {
		name   string
		result ToolResult
		code   string
	}{
		{"text", ToolResult{IsError: true, Content: "denied", Diagnostics: []ToolDiagnostic{{Severity: "error", Message: "ignored"}}}, `try{await tools.leaf({});throw Error("resolved")}catch(e){if(!(e instanceof Error)||e.message!=="denied")throw Error("wrong rejection")};return "ok";`},
		{"diagnostics", ToolResult{IsError: true, Diagnostics: []ToolDiagnostic{{Severity: "warn", Message: "ignored"}, {Severity: "error", Message: "first"}, {Severity: "error", Message: "second"}}}, `try{await tools.leaf({});throw Error("resolved")}catch(e){if(e.message!=="first\nsecond")throw Error("wrong diagnostics")};return "ok";`},
		{"empty", ToolResult{IsError: true}, `try{await tools.leaf({});throw Error("resolved")}catch(e){if(e.message!=="Tool leaf failed")throw Error("empty failure resolved")};return "ok";`},
		{"structured", ToolResult{IsError: true, HasStructuredOutput: true, StructuredOutput: JSON{"code": 7}}, `const v=await tools.leaf({});if(v.code!==7)throw Error("structured error rejected");return "ok";`},
		{"null", ToolResult{IsError: true, HasStructuredOutput: true, StructuredOutput: nil}, `if(await tools.leaf({})!==null)throw Error("null lost");return "ok";`},
		{"schemaless-properties", ToolResult{Content: "failed", StructuredOutput: JSON{"reason": "detail"}, HasStructuredOutput: true, IsError: true}, `try{await tools.leaf({});throw Error("resolved")}catch(e){if(e.message!=="failed"||e.structuredOutput.reason!=="detail")throw Error("properties lost")};return "ok";`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var effects atomic.Int64
				leaf := wrapRegistration("leaf")
				if tc.name == "structured" {
					leaf.StructuredOutputSchema = JSON{"type": "object"}
				}
				if tc.name == "null" {
					leaf.StructuredOutputSchema = JSON{"type": "null"}
				}
				leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { effects.Add(1); return tc.result, nil }
				if err := registry.Register(leaf); err != nil {
					t.Fatal(err)
				}
				def := taskDefinition(t, "error-outcome", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					for i := 0; i < 2; i++ {
						result, err := r.RunCodemode(ctx, "script", tc.code, sandbox)
						if err != nil {
							return err
						}
						if result.Error != "" || !result.HasValue || result.Value != "ok" {
							t.Errorf("outcome %+v", result)
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, err := registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				h := openHarness(t, b.store, Options{Registry: registry})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				record := waitPublicTask(t, h, id)
				if record.State.Outcome.Status != "completed" || effects.Load() != 1 {
					t.Fatalf("outcome=%+v error=%+v effects=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load())
				}
			})
		})
	}
}
