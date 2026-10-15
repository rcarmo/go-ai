package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNestedTool120RejectedProgressFailsSessionWithoutAdoption(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		cause := reject("nested progress rejected")
		reported := make(chan error, 2)
		var h *Harness
		var childID atomic.Uint64
		var attempts atomic.Int64
		var leaked atomic.Bool
		reg := wrapRegistration("nested_progress_fault")
		reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			id := api.TaskID()
			childID.Store(uint64(id))
			core := h.session.core
			installTaskAppend(t, h, core, func(kind byte, _ uint64, _ uint64, payload []byte) error {
				if kind == 2 {
					var commit commitRecord
					if e := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &commit); e != nil {
						return e
					}
					for _, write := range commit.Writes {
						if write.Task != nil && write.Task.ID == id && write.Task.Execution.Native.State.Checkpoint["progress"] != nil {
							attempts.Add(1)
							return cause
						}
					}
				}
				return nil
			})
			e := api.Output("not-adopted")
			if !errors.Is(e, cause) {
				t.Errorf("progress error lost:%v", e)
			}
			return ToolResult{}, e
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "nested-progress-fault-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, e := r.CallTool(ctx, "key", "nested_progress_fault", JSON{})
			return e
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h = openHarness(t, b.store, Options{Registry: registry, OnReport: func(e error) {
			select {
			case reported <- e:
			default:
			}
		}})
		parent := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		baseline, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		watch, err := h.WatchEvents(bg, baseline.Tasks[parent].Conversation)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		if err := watch.Start(func(_ context.Context, events []AgentEvent) error {
			for _, event := range events {
				if event.Output == "not-adopted" || event.Type == "tool_execution_end" && event.ToolName == "nested_progress_fault" {
					leaked.Store(true)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		end, e := h.session.Closed(ctx)
		if e != nil || end.Reason != "failed" || !errors.Is(end.Error, cause) {
			t.Fatal(end, e)
		}
		if e = h.Close(ctx); e != nil {
			t.Fatal(e)
		}
		if leaked.Load() {
			t.Fatal("rejected progress/result leaked to subscribers")
		}
		reopened := reopenStoreAfterHarnessClose(t, b.store)
		defer reopened.Close(bg)
		state, e := reopened.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		task := state.Tasks[ID(childID.Load())]
		if taskHasDecidedOutcome(task) || task.Execution.Native.State.Checkpoint["progress"] != nil || attempts.Load() != 1 {
			t.Fatal("rejected progress adopted/retried", task, attempts.Load())
		}
		for _, doc := range state.Documents {
			if doc.Kind == nestedCallDocument {
				var call nestedCallState
				if e = fromObject(doc.Value, &call, h.session.limits); e != nil || call.Result != nil {
					t.Fatal("progress failure adopted result", call, e)
				}
			}
		}
	})
}
