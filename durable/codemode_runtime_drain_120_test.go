package durable

import (
	"context"
	"testing"
)

func TestCodemode120RuntimePersistsQuickJSDrains(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		sandbox, err := NewQuickJSSandbox(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer sandbox.Close(context.Background())
		sandbox.AcknowledgeDrains = true
		registry := NewRegistry()
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "ok"}, nil }
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "drain-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `await tools.codemode_leaf({});return "done";`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" {
				t.Fatal(result.Error)
			}
			snap, err := r.harness.Snapshot(ctx)
			if err != nil {
				return err
			}
			found := false
			for _, doc := range snap.Documents {
				if doc.Owner == r.taskID && doc.Kind == codemodeJournalKind {
					var j codemodeJournalState
					if err := fromObject(doc.Value, &j, r.harness.session.limits); err != nil {
						return err
					}
					if j.FirstIssued == nil || *j.FirstIssued != 1 || len(j.Deliveries) != 1 || j.Deliveries[0].IssuedAfter == nil || *j.Deliveries[0].IssuedAfter != 1 {
						t.Fatal("missing persisted drains", j)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("missing journal")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
