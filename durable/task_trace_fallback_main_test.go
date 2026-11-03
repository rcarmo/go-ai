package durable

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived scheduler.ts2226-2229: unavailable extraction uses the host
// root, even if a process-local creator handle happens to remain alive.
func TestMainTaskTraceUnavailableExtractionUsesRoot(t *testing.T) {
	for _, mode := range []string{"absent", "nil", "panic"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				backend := newMainTaskTraceBackend(t)
				switch mode {
				case "absent":
					backend.root.ExtractContext = nil
				case "nil":
					backend.root.ExtractContext = func(goai.TelemetryCarrier) *goai.TelemetrySpan { return nil }
				case "panic":
					backend.root.ExtractContext = func(goai.TelemetryCarrier) *goai.TelemetrySpan { panic("backend unavailable") }
				}
				def := taskDefinition(t, "task.trace-root-fallback", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				h := taskTestHarnessOptions(t, b.store, Options{Telemetry: backend.root}, def)
				backend.h = h
				creator := backend.create("creator", nil, nil, nil)
				carrier := &invocationTelemetry{}
				carrier.span.Store(creator)
				ctx := context.WithValue(bg, telemetryContextKey{}, carrier)
				var id ID
				_, err := h.CommitTasks(ctx, 1, func(tx *Tx) error {
					var e error
					id, e = tx.CreateTask(def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					return e
				})
				if err != nil {
					t.Fatal(err)
				}
				creator.End()
				record := waitPublicTask(t, h, id)
				mainTaskTraceFields(t, record, creator.Context, nil)
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				backend.mu.Lock()
				defer backend.mu.Unlock()
				if len(backend.spans) != 2 {
					t.Fatal("phase count", len(backend.spans))
				}
				if backend.spans[1].parent != nil {
					t.Error("unavailable extraction used creator instead of root", backend.spans[1].parent)
				}
			})
		})
	}
}
