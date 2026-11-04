package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

// Source codemode.test.ts:663: the first undefined global result is journalled
// before a replay-safe nested call blocks; after close/reopen the first result
// is replayed, the held call resumes, and the next global result is undefined.
func TestMainCodemodeGlobalUndefinedAcrossBlockedToolReopen(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	entered := make(chan struct{}, 1)
	var blocked atomic.Bool
	blocked.Store(true)
	var globals, holds atomic.Int64
	hold := wrapRegistration("codemode_hold")
	hold.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
		holds.Add(1)
		if blocked.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		}
		return ToolResult{Content: "held"}, nil
	}
	if err = registry.Register(hold); err != nil {
		t.Fatal(err)
	}
	def := taskDefinition(t, "codemode.undefined.midflight", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.RunCodemode(ctx, "run", `const before=await host.nothing(null);await tools.codemode_hold({});return [before===undefined,(await host.nothing(null))===undefined];`, engine)
		if err != nil {
			return err
		}
		if result.Error != "" || !equalJSONValue(result.Value, []any{true, true}) {
			t.Errorf("undefined across tool replay: %+v", result)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err = registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{{Name: "host.nothing", Version: 1, Execute: func(context.Context, any) (any, error) { globals.Add(1); return CodemodeUndefined, nil }}}})
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err = h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	if globals.Load() != 1 || holds.Load() != 1 {
		t.Fatalf("before close globals=%d holds=%d", globals.Load(), holds.Load())
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, doc := range state.Documents {
		if doc.Kind == codemodeJournalKind {
			var journal codemodeJournalState
			if err := fromObject(doc.Value, &journal, h.session.limits); err != nil {
				t.Fatal(err)
			}
			if len(journal.Deliveries) != 1 || journal.Deliveries[0].HasValue {
				t.Fatalf("first global not journalled as undefined: %+v", journal.Deliveries)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing journal")
	}
	if err = h.Close(bg); err != nil {
		t.Fatal(err)
	}
	blocked.Store(false)
	next := openHarness(t, reopenStoreAfterHarnessClose(t, store), h.options)
	record := waitPublicTask(t, next, id)
	if record.State.Outcome.Status != "completed" || globals.Load() != 2 || holds.Load() != 2 {
		t.Fatalf("outcome=%+v globals=%d holds=%d", record.State.Outcome, globals.Load(), holds.Load())
	}
}
