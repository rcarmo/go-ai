package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Source codemode.test.ts:673. A slow global returns only after cancellation;
// the fast global settles the script first. Go joins host callbacks before
// returning instead of relying on JavaScript unhandled-rejection events.
func TestMainCodemodeLateGlobalResultIsNotJournalled(t *testing.T) {
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
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var lateReturns atomic.Int64
	def := taskDefinition(t, "codemode.global.late", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.RunCodemode(ctx, "run", `const pending=host.slowly(null);await host.quickly(null);return 1;`, engine)
		if err != nil {
			return err
		}
		if result.Error != "" || !equalJSONValue(result.Value, 1) {
			t.Errorf("script result %+v", result)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err = registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{
		{Name: "host.slowly", Version: 1, Execute: func(ctx context.Context, _ any) (any, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			lateReturns.Add(1)
			return "late", nil
		}},
		{Name: "host.quickly", Version: 1, Execute: func(ctx context.Context, _ any) (any, error) {
			select {
			case <-entered:
				return "quick", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}},
	}})
	t.Cleanup(unblock)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err = h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, cancelled)
	before, err := h.Snapshot(bg)
	if err != nil {
		unblock()
		t.Fatal(err)
	}
	for _, doc := range before.Documents {
		if doc.Kind == codemodeJournalKind {
			var journal codemodeJournalState
			if err := fromObject(doc.Value, &journal, h.session.limits); err != nil {
				unblock()
				t.Fatal(err)
			}
			for _, d := range journal.Deliveries {
				if d.Call.Name == "host.slowly" {
					unblock()
					t.Fatal("slow result journalled before release")
				}
			}
		}
	}
	unblock()
	record := waitPublicTask(t, h, id)
	if record.State.Outcome.Status != "completed" || lateReturns.Load() != 1 {
		t.Fatalf("outcome=%+v late returns=%d", record.State.Outcome, lateReturns.Load())
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
			if len(journal.Issued) != 2 || len(journal.Deliveries) != 1 || journal.Deliveries[0].Call.Name != "host.quickly" {
				t.Fatalf("late journal %+v", journal)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("journal missing")
	}
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
