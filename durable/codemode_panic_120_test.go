package durable

import (
	"context"
	"strings"
	"testing"
)

type panicSandbox120 struct{ mode string }

func (s panicSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	if s.mode == "start" {
		panic("private host data")
	}
	return panicExecution120(s), nil
}

type panicExecution120 struct{ mode string }

func (e panicExecution120) Next(context.Context) (CodemodeEvent, error) {
	if e.mode == "next" {
		panic("private host data")
	}
	return CodemodeEvent{Call: &CodemodeCall{ID: 1, Name: "codemode_leaf", Arguments: JSON{}}}, nil
}
func (e panicExecution120) Deliver(context.Context, CodemodeDelivery) error {
	panic("private host data")
}
func (e panicExecution120) Close() error { panic("private close data") }
func TestCodemode120HostPanicContainmentAndAdmissionRelease(t *testing.T) {
	for _, mode := range []string{"start", "next", "deliver"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				reg := wrapRegistration("codemode_leaf")
				reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "leaf"}, nil }
				if e := registry.Register(reg); e != nil {
					t.Fatal(e)
				}
				def := taskDefinition(t, "sandbox-panic-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					_, e := r.RunCodemode(ctx, "run", "source", panicSandbox120{mode})
					if e == nil || strings.Contains(e.Error(), "private") {
						t.Fatal("panic boundary", e)
					}
					_, e = r.RunCodemode(ctx, "run", "source", panicSandbox120{mode})
					if e == nil || strings.Contains(e.Error(), "already active") {
						t.Fatal("driver admission leaked", e)
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
		})
	}
}
