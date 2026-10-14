package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSJournalAndSettledReuse(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		reg := wrapRegistration("codemode_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if err := registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "wasm-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := `const v=await tools.codemode_leaf({});text(v);store("key",{n:1});`
			first, err := r.RunCodemode(ctx, "run", code, sandbox)
			if err != nil {
				return err
			}
			if first.Error != "" || first.Output != "leaf" {
				t.Errorf("VM result %+v", first)
			}
			second, err := r.RunCodemode(ctx, "run", code, sandbox)
			if err != nil {
				return err
			}
			if second.Output != first.Output || effects.Load() != 1 {
				t.Error("settled effect replay", second, effects.Load())
			}
			deleted, err := r.RunCodemode(ctx, "delete", `store("key",undefined);store("null",null);return null;`, sandbox)
			if err != nil {
				return err
			}
			if deleted.Error != "" || !deleted.HasValue || deleted.Value != nil {
				t.Error("null return or delete", deleted)
			}
			failed, err := r.RunCodemode(ctx, "failed-delete", `store("null",undefined);throw new Error("failed");`, sandbox)
			if err != nil {
				return err
			}
			if failed.Error == "" {
				t.Error("script error missing")
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				store, err := tx.AcquireDocument(codemodeStoreDefinition(), 1, nil, nil)
				if err != nil {
					return nil, err
				}
				object, err := store.Get()
				if err != nil {
					return nil, err
				}
				values := object["values"].(map[string]any)
				if _, ok := values["key"]; ok {
					t.Error("deleted value persisted")
				}
				if value, ok := values["null"]; !ok || value != nil {
					t.Error("null confused with deletion or failed deletion adopted", values)
				}
				return taskDone(nil), nil
			})
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
