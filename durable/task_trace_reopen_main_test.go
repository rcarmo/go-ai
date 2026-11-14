package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Native controls for confirmed storage refusal, then real journal/SQLite
// reopen. Context extraction must not depend on any original process span.
func TestMainTaskTraceRefusalAndReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		backend := newMainTaskTraceBackend(t)
		def := taskDefinition(t, "task.trace-reopen", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: backend.root}, def)
		backend.h = h
		parent := goai.TelemetryCarrier{"scope": "previous-process", "span": "parent"}
		linked := goai.TelemetryCarrier{} // Present-empty is not an absent link.
		optionsJSON, _ := json.Marshal(map[string]any{"trace": parent, "links": []goai.TelemetryCarrier{linked}})
		options := mainTaskTraceOptions(t, string(optionsJSON))
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		refused := &StorageRejected{Reason: "task trace adoption refused"}
		core := h.session.core
		original := core.appendFrame
		core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				return refused
			}
			if original != nil {
				return original(kind, ordinal, high, payload)
			}
			return nil
		}
		var failed ID
		_, err = h.commitTasks(bg, 1, func(tx *Tx) error { var e error; failed, e = tx.CreateTask(def, nil, options); return e })
		core.appendFrame = original
		if !errors.Is(err, refused) {
			t.Fatal("confirmed refusal", err)
		}
		// Refusal fails/closes the Session asynchronously. Inspect durable
		// rollback after joining close and reopening, not by racing Snapshot
		// against the old store's automatic shutdown.
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := b.reopen()
		after, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := after.Tasks[failed]; exists || after.Seq != before.Seq || len(h.telemetryParents) != 0 {
			t.Fatal("refused task trace leaked", failed)
		}
		backend.mu.Lock()
		n := len(backend.spans)
		backend.mu.Unlock()
		if n != 0 {
			t.Fatal("refused task dispatched telemetry", n)
		}
		h = taskTestHarnessOptions(t, store, Options{Telemetry: backend.root}, def)
		backend.h = h
		id := createPublicTask(t, h, def, nil, options)
		record, _, err := h.Task(bg, id)
		if err != nil {
			t.Fatal(err)
		}
		mainTaskTraceFields(t, record, parent, []goai.TelemetryCarrier{linked})
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := b.reopen()
		fresh := newMainTaskTraceBackend(t)
		// A different backend rehydrates a data-only context. No pointer, callback,
		// Value, prior record or original backend is consulted during extraction.
		extract := func(c goai.TelemetryCarrier) *goai.TelemetrySpan {
			if fresh.h != nil {
				if _, err := fresh.h.Snapshot(bg); err != nil {
					t.Error("extract on Session line", err)
				}
			}
			owned := mainTaskTraceCopy(c)
			if c != nil {
				c["mutation"] = "backend must not mutate records"
			}
			return &goai.TelemetrySpan{Context: owned, StartSpan: func(n string, a map[string]any) *goai.TelemetrySpan { return fresh.create(n, owned, a, nil) }}
		}
		field := reflect.ValueOf(fresh.root).Elem().FieldByName("ExtractContext")
		if field.IsValid() {
			field.Set(reflect.ValueOf(extract))
		}
		next := taskTestHarnessOptions(t, reopened, Options{Telemetry: fresh.root}, def)
		fresh.h = next
		final := waitPublicTask(t, next, id)
		mainTaskTraceFields(t, final, parent, []goai.TelemetryCarrier{linked})
		if err := next.Close(bg); err != nil {
			t.Fatal(err)
		}
		fresh.mu.Lock()
		defer fresh.mu.Unlock()
		if len(fresh.spans) != 1 {
			t.Fatal("fresh phase count", len(fresh.spans))
		}
		phase := fresh.spans[0]
		if !phase.ended || !reflect.DeepEqual(phase.parent, parent) || !reflect.DeepEqual(phase.links, []goai.TelemetryCarrier{linked}) || phase.attrs["resumed"] != true {
			t.Error("reopened phase", phase)
		}
	})
}

// Source-derived scheduler fallback: a creator with no propagation data must
// not manufacture a persisted carrier or override the harness root.
func TestMainTaskTraceCreatorWithoutCarrier(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		backend := newMainTaskTraceBackend(t)
		def := taskDefinition(t, "task.trace-no-carrier", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: backend.root}, def)
		backend.h = h
		creator := backend.create("creator", nil, nil, nil)
		creator.Context = nil
		carrier := &invocationTelemetry{}
		carrier.span.Store(creator)
		ctx := context.WithValue(bg, telemetryContextKey{}, carrier)
		var id ID
		_, err := h.commitTasks(ctx, 1, func(tx *Tx) error {
			var e error
			id, e = tx.CreateTask(def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		creator.End()
		record := waitPublicTask(t, h, id)
		mainTaskTraceFields(t, record, nil, nil)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		backend.mu.Lock()
		defer backend.mu.Unlock()
		if len(backend.spans) != 2 {
			t.Fatal("phase count", len(backend.spans))
		}
		if phase := backend.spans[1]; phase.parent != nil {
			t.Error("carrierless creator incorrectly parented phase", phase.parent)
		}
	})
}

// Source scheduler keeps links for all phases of the first invocation only.
// Parking and reserving a second invocation is an actual scheduler transition.
func TestMainTaskTraceLinksFirstInvocationOnly(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		backend := newMainTaskTraceBackend(t)
		var invocations atomic.Int64
		def := taskDefinition(t, "task.trace-first-only", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			n := invocations.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				if n == 1 {
					until := int64(100)
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "work"}, Until: &until}, nil
				}
				return taskDone(nil), nil
			})
		})
		var clock atomic.Int64
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: backend.root, Now: clock.Load}, def)
		backend.h = h
		linked := backend.create("linked", nil, nil, nil)
		linked.End()
		raw, _ := json.Marshal(map[string]any{"links": []goai.TelemetryCarrier{linked.Context}})
		id := createPublicTask(t, h, def, nil, mainTaskTraceOptions(t, string(raw)))
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			parked := false
			if err := h.session.readTasks(bg, func(s Snapshot) error {
				parked = s.Tasks[id].Status == "sleeping" && h.scheduler.invocations[id] == nil
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if parked {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("task did not park")
			}
			time.Sleep(time.Millisecond)
		}
		clock.Store(100)
		h.notify()
		waitPublicTask(t, h, id)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		backend.mu.Lock()
		defer backend.mu.Unlock()
		var phases []*mainTaskTraceSpan
		for _, s := range backend.spans {
			if s.name == "pi.durable.phase" {
				phases = append(phases, s)
			}
		}
		if len(phases) != 2 {
			t.Fatal("invocation phases", len(phases))
		}
		if !reflect.DeepEqual(phases[0].links, []goai.TelemetryCarrier{linked.Context}) || phases[1].links != nil {
			t.Error("first-invocation link lifetime", phases[0].links, phases[1].links)
		}
	})
}
