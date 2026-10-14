package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSFailureReportsPriorEffects(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		var effects atomic.Int64
		registry := NewRegistry()
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "failure-output-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := `await tools.codemode_leaf({});console.log("before failure");store("bad",true);throw new TypeError("boom");`
			result, err := r.RunCodemode(ctx, "run", code, sandbox)
			if err != nil {
				return err
			}
			if result.Error == "" || !strings.Contains(result.Output, "TypeError: boom") || !strings.Contains(result.Output, "codemode_leaf (ok)") || !strings.Contains(result.Output, "they are not undone") || !strings.Contains(result.Output, "<console_output>") {
				t.Error(result)
			}
			if _, err = r.RunCodemode(ctx, "run", code, sandbox); err != nil {
				return err
			}
			if effects.Load() != 1 {
				t.Error("failed replay repeated effect")
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				doc, err := tx.AcquireDocument(codemodeStoreDefinition(), 1, nil, nil)
				if err != nil {
					return nil, err
				}
				object, err := doc.Get()
				if err != nil {
					return nil, err
				}
				if len(object["values"].(map[string]any)) != 0 {
					t.Error("failed store adopted")
				}
				return taskDone(nil), nil
			})
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
