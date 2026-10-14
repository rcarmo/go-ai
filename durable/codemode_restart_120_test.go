package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

type restartSandbox120 struct {
	starts            atomic.Int64
	entered, returned chan struct{}
	clock             int64
	seed              uint64
}

func (s *restartSandbox120) Start(_ context.Context, _ string, start CodemodeStart) (CodemodeExecution, error) {
	n := s.starts.Add(1)
	if n == 1 {
		s.clock = start.StartedAt
		s.seed = start.Seed
	}
	return &restartExecution120{parent: s, first: n == 1, start: start}, nil
}

type restartExecution120 struct {
	parent *restartSandbox120
	first  bool
	start  CodemodeStart
	step   int
}

func (e *restartExecution120) Next(ctx context.Context) (CodemodeEvent, error) {
	if e.step == 0 {
		return CodemodeEvent{Call: &CodemodeCall{ID: 1, Name: "codemode_leaf", Arguments: JSON{"n": 1}}}, nil
	}
	if e.first {
		close(e.parent.entered)
		<-ctx.Done()
		close(e.parent.returned)
		return CodemodeEvent{}, ctx.Err()
	}
	return CodemodeEvent{Result: &CodemodeResult{Output: "reopened"}}, nil
}
func (e *restartExecution120) Deliver(context.Context, CodemodeDelivery) error { e.step++; return nil }
func (e *restartExecution120) Close() error                                    { return nil }
func TestCodemode120ActualCallerRestartRetainsJournalWithoutEffects(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		reg := wrapRegistration("codemode_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		sandbox := &restartSandbox120{entered: make(chan struct{}), returned: make(chan struct{})}
		def := taskDefinition(t, "codemode-restart-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, e := r.RunCodemode(ctx, "run", "source", sandbox)
			if e != nil {
				return e
			}
			if result.Output != "reopened" {
				t.Fatal(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, sandbox.entered)
		if e := h.Close(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, sandbox.returned)
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("%+v %+v effects:%d starts:%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), sandbox.starts.Load())
		}
	})
}
func TestCodemode120JournalMutationsRejected(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		reg := wrapRegistration("codemode_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "leaf"}, nil }
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "journal-integrity-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			sandbox := &serialSandbox120{failDelivery: true}
			if _, e := r.RunCodemode(ctx, "run", "source", sandbox); e == nil {
				t.Fatal("missing VM loss")
			}
			for _, mode := range []string{"source", "seed", "issued", "delivery", "retire"} {
				e := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					for _, doc := range tx.state.Documents {
						if doc.Owner != r.taskID || doc.Key != "run" {
							continue
						}
						if (mode == "source" || mode == "seed") && doc.Kind == codemodeRunKind {
							handle, e := tx.Document(doc.ID)
							if e != nil {
								return nil, e
							}
							value, e := handle.Get()
							if e != nil {
								return nil, e
							}
							if mode == "source" {
								value["source"] = "forged"
							} else {
								value["start"].(map[string]any)["seed"] = 0
							}
							return nil, handle.Set(value)
						}
						if doc.Kind == codemodeJournalKind && mode != "source" && mode != "seed" {
							handle, e := tx.Document(doc.ID)
							if e != nil {
								return nil, e
							}
							value, e := handle.Get()
							if e != nil {
								return nil, e
							}
							switch mode {
							case "issued":
								value["issued"] = []any{}
							case "delivery":
								value["deliveries"].([]any)[0].(map[string]any)["value"] = "forged"
							case "retire":
								return nil, handle.Retire()
							}
							return nil, handle.Set(value)
						}
					}
					return nil, reject("fixture missing")
				})
				if e == nil {
					t.Fatal("journal mutation accepted", mode)
				}
			}
			if _, e := r.RunCodemode(ctx, "run", "source", sandbox); e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
