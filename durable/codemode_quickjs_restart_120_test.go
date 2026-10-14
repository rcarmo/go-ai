package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

type restartQuickJS120 struct {
	sandbox *QuickJSSandbox
	starts  atomic.Int64
	ready   chan struct{}
}

func (s *restartQuickJS120) RuntimeIdentity() string { return s.sandbox.RuntimeIdentity() }
func (s *restartQuickJS120) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	vm, err := s.sandbox.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	return &restartQuickJSExecution120{CodemodeExecution: vm, first: s.starts.Add(1) == 1, ready: s.ready}, nil
}

type restartQuickJSExecution120 struct {
	CodemodeExecution
	first bool
	ready chan struct{}
}

func (e *restartQuickJSExecution120) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if err == nil && event.Result != nil && e.first {
		close(e.ready)
		<-ctx.Done()
		return CodemodeEvent{}, ctx.Err()
	}
	return event, err
}
func TestCodemode120QuickJSUnsettledCallerReopen(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		sandbox := &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}
		registry := NewRegistry()
		var effects atomic.Int64
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "wasm-reopen-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const before=Math.random();const ns=await describeNamespace("leaf_tools");if(ns.description!=="original")throw Error("namespace drift");const matches=await searchTools("leaf");if(matches[0].name!=="codemode_leaf")throw Error("discovery");const v=await tools.codemode_leaf({n:before});text(v);store("clock",Date.now());return before;`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Output != "==> text 1/2 <==\nleaf\n==> text 2/2 <==\n"+fmt.Sprint(result.Value) || !result.HasValue {
				t.Errorf("reopen result %+v", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, CodemodeNamespaces: []CodemodeNamespace{{Name: "leaf-tools", Prefix: "codemode_", Description: "original"}}})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, doc := range state.Documents {
			if doc.Kind == codemodeJournalKind {
				var journal codemodeJournalState
				if err = fromObject(doc.Value, &journal, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if len(journal.Issued) != 3 || len(journal.Deliveries) != 3 || !journal.Issued[0].Global || !journal.Issued[1].Global || journal.Issued[2].Global {
					t.Fatal("mixed journal missing", journal)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("journal missing")
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		options := h.options
		options.CodemodeNamespaces = []CodemodeNamespace{{Name: "leaf-tools", Prefix: "codemode_", Description: "changed"}}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("%+v %+v effects=%d starts=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), sandbox.starts.Load())
		}
	})
}
