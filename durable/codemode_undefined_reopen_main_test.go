package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

// Source codemode.test.ts:663: a global's undefined result is preserved live
// and from the stored journal after a real Harness reopen. Go uses an explicit
// sentinel because nil remains the existing JSON-null result.
func TestMainCodemodeGlobalUndefinedAndNullReopen(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	sandbox := &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	var undefinedCalls, nullCalls atomic.Int64
	def := taskDefinition(t, "codemode.undefined.reopen", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.RunCodemode(ctx, "run", `const before=await host.nothing(null);const n=await host.null(null);const after=await host.nothing(null);return [before===undefined,n===null,after===undefined];`, sandbox)
		if err != nil {
			return err
		}
		if result.Error != "" || !equalJSONValue(result.Value, []any{true, true, true}) {
			t.Errorf("undefined/null result: %+v", result)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err = registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{
		{Name: "host.nothing", Version: 1, Execute: func(context.Context, any) (any, error) { undefinedCalls.Add(1); return CodemodeUndefined, nil }},
		{Name: "host.null", Version: 1, Execute: func(context.Context, any) (any, error) { nullCalls.Add(1); return nil, nil }},
	}})
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err = h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, sandbox.ready)
	if undefinedCalls.Load() != 2 || nullCalls.Load() != 1 {
		t.Fatalf("live calls %d %d", undefinedCalls.Load(), nullCalls.Load())
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var journal codemodeJournalState
	found := false
	for _, doc := range state.Documents {
		if doc.Kind == codemodeJournalKind {
			if err := decodeDocumentValue(doc, h.session.limits, &journal); err != nil {
				t.Fatal(err)
			}
			found = true
		}
	}
	if !found || len(journal.Deliveries) != 3 || journal.Deliveries[0].HasValue || !journal.Deliveries[1].HasValue || journal.Deliveries[1].Value != nil || journal.Deliveries[2].HasValue {
		t.Fatalf("journal undefined/null: %+v", journal.Deliveries)
	}
	if err = h.Close(bg); err != nil {
		t.Fatal(err)
	}
	next := openHarness(t, reopenStoreAfterHarnessClose(t, store), h.options)
	record := waitPublicTask(t, next, id)
	if record.State.Outcome.Status != "completed" || undefinedCalls.Load() != 2 || nullCalls.Load() != 1 || sandbox.starts.Load() != 2 {
		t.Fatalf("outcome=%+v undefined=%d null=%d starts=%d", record.State.Outcome, undefinedCalls.Load(), nullCalls.Load(), sandbox.starts.Load())
	}
}
