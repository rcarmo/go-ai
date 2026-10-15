package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
	"time"
)

func TestNestedTool120CommittedLifecycleEvents(t *testing.T) { testNestedLifecycle120(t, false, false) }
func TestNestedTool120BlockedLifecycleEvents(t *testing.T)   { testNestedLifecycle120(t, true, false) }
func TestNestedTool120RewrittenArgumentEvents(t *testing.T)  { testNestedLifecycle120(t, false, true) }
func testNestedLifecycle120(t *testing.T, blocked, rewrite bool) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		leaf := wrapRegistration("event_leaf")
		leaf.Execute = func(ctx context.Context, arguments JSON, api *ToolAPI) (ToolResult, error) {
			if rewrite && !equalJSONValue(arguments, JSON{"n": 2}) {
				t.Error("executor rewrite missing", arguments)
			}
			if err := api.Output("progress"); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: "finished"}, nil
		}
		if rewrite {
			if err := registry.Install(&Extension{Name: "rewrite-nested", ToolHooks: ToolHooks{BeforeTool: func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error) {
				return &BeforeToolResult{Arguments: JSON{"n": 2}}, nil
			}}}); err != nil {
				t.Fatal(err)
			}
		}
		if blocked {
			leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
				t.Error("blocked executor ran")
				return ToolResult{}, nil
			}
			if err := registry.Install(&Extension{Name: "block-nested", ToolHooks: ToolHooks{BeforeTool: func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error) {
				return &BeforeToolResult{Block: "blocked"}, nil
			}}}); err != nil {
				t.Fatal(err)
			}
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
		finalUpdate := false
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
						want := 1
						if rewrite {
							want = 2
						}
						if !equalJSONValue(event.Args, JSON{"n": want}) {
							t.Error(event.Args)
						}
					case "tool_execution_update":
						updates++
						if event.Output == "finished" || blocked && event.Output == "Tool blocked" {
							finalUpdate = true
						}
					case "tool_execution_end":
						ends++
						want := "finished"
						if blocked {
							want = "Tool blocked"
						}
						if event.NestedResult == nil || event.NestedResult.Content != want || event.NestedResult.IsError != blocked {
							t.Error("nested result absent", event.NestedResult)
						}
					}
				}
			case <-deadline:
				end, ended := stream.End()
				t.Fatal("missing nested lifecycle", starts, updates, ends, end, ended)
			}
		}
		wantStarts := 1
		if blocked {
			wantStarts = 0
		}
		if starts != wantStarts || !blocked && updates < 1 || ends != 1 || !finalUpdate {
			t.Fatal("nested lifecycle counts", starts, updates, ends)
		}
	})
}

func TestNestedTool120EventDepthUsesOwnership(t *testing.T) {
	state := Snapshot{Tasks: map[ID]Task{1: {ID: 1}, 2: {ID: 2, Owner: 1}, 3: {ID: 3, Owner: 2}}}
	if eventTaskDepth(state, 3) != 3 || eventTaskDepth(state, 2) != 2 || eventTaskDepth(state, 99) != 0 {
		t.Fatal("incorrect event ownership depth")
	}
}
