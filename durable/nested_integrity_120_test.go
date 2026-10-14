package durable

import (
	"context"
	"testing"
)

func TestNestedTool120RejectsRetargetForgedResultAndInputMutation(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		reg := wrapRegistration("integrity_child")
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "integrity-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			r.harness.session.taskBookkeeping(func() { r.harness.scheduler.enabled.Store(false) })
			handle, e := r.StartTool(ctx, "stable", "integrity_child", JSON{})
			if e != nil {
				return e
			}
			// Keep scheduler selection disabled: local commits exercise an admitted,
			// nonexecuted child's authority without racing genuine settlement.
			r.harness.session.taskBookkeeping(func() { r.harness.scheduler.enabled.Store(false) })
			for _, mode := range []string{"retarget", "result", "input", "retire"} {
				e = r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					doc, err := tx.Document(handle.document)
					if err != nil {
						return nil, err
					}
					value, err := doc.Get()
					if err != nil {
						return nil, err
					}
					switch mode {
					case "retarget":
						value["task"] = 1
						return nil, doc.Set(value)
					case "result":
						value["result"] = JSON{"content": "forged", "isError": false, "durationMs": 0}
						return nil, doc.Set(value)
					case "retire":
						return nil, doc.Retire()
					case "input":
						task, err := copyTask(tx.state.Tasks[handle.task], tx.limits)
						if err != nil {
							return nil, err
						}
						task.Execution.Native.Input.Value.(map[string]any)["key"] = "changed"
						return nil, tx.stage(Write{Op: "put-task", Task: &task})
					}
					return nil, nil
				})
				if e == nil {
					t.Error("nested integrity mutation accepted", mode)
				}
			}
			r.harness.session.taskBookkeeping(func() { r.harness.scheduler.enabled.Store(true) })
			r.harness.scheduler.kick()
			if _, e = handle.Wait(ctx); e != nil {
				return e
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
