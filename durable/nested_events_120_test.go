package durable

import (
	"context"
	"testing"
	"time"
)

func TestNestedTool120CommittedLifecycleEvents(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		leaf := wrapRegistration("event_leaf")
		leaf.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			if err := api.Output("progress"); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: "finished"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "nested-events-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.CallTool(ctx, "child", "event_leaf", JSON{"n": 1}); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		snapshot, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		stream, err := h.WatchEvents(bg, snapshot.Tasks[id].Conversation)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Stop()
		received := make(chan []AgentEvent, 128)
		if err := stream.Start(func(_ context.Context, events []AgentEvent) error { received <- events; return nil }); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
		starts, updates, ends := 0, 0, 0
		deadline := time.After(3 * time.Second)
		for ends == 0 {
			select {
			case events := <-received:
				for _, event := range events {
					if event.ToolName != "event_leaf" {
						continue
					}
					if event.ParentTask != id || event.ToolCallID != "child" {
						t.Error("nested event attribution", event)
					}
					switch event.Type {
					case "tool_execution_start":
						starts++
						if !equalJSONValue(event.Args, JSON{"n": 1}) {
							t.Error(event.Args)
						}
					case "tool_execution_update":
						updates++
					case "tool_execution_end":
						ends++
						if event.NestedResult == nil || event.NestedResult.Content != "finished" {
							t.Error("nested result absent", event.NestedResult)
						}
					}
				}
			case <-deadline:
				end, ended := stream.End()
				t.Fatal("missing nested lifecycle", starts, updates, ends, end, ended)
			}
		}
		if starts != 1 || updates < 1 || ends != 1 {
			t.Fatal("nested lifecycle counts", starts, updates, ends)
		}
	})
}
