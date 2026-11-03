package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived scheduler.ts2214-2240: extraction belongs to each phase, not
// merely invocation reservation. Preserve backend callback ordering as well
// as the resulting parent/link contexts.
func TestMainTaskTraceExtractionPerPhase(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		backend := newMainTaskTraceBackend(t)
		parent := backend.create("parent", nil, nil, nil)
		parent.End()
		linked := backend.create("linked", nil, nil, nil)
		linked.End()
		original := backend.root.ExtractContext
		var extracted []goai.TelemetryCarrier
		backend.root.ExtractContext = func(c goai.TelemetryCarrier) *goai.TelemetrySpan {
			extracted = append(extracted, mainTaskTraceCopy(c))
			return original(c)
		}
		def, err := DefineTask(TaskDefinitionOptions{Kind: "task.trace-extraction", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
			"work": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish"}}, nil
				})
			},
			"finish": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: backend.root}, def)
		backend.h = h
		raw, _ := json.Marshal(map[string]any{"trace": parent.Context, "links": []goai.TelemetryCarrier{linked.Context}})
		id := createPublicTask(t, h, def, nil, mainTaskTraceOptions(t, string(raw)))
		waitPublicTask(t, h, id)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		want := []goai.TelemetryCarrier{parent.Context, linked.Context, parent.Context, linked.Context}
		if !reflect.DeepEqual(extracted, want) {
			t.Error("phase extraction sequence", extracted, "want", want)
		}
	})
}
