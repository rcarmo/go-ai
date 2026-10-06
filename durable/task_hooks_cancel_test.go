package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestTaskHooksCancellationStopsLaterHandlersAndReportsCallbackErrors(t *testing.T) {
	registry := NewRegistry()
	entered := make(chan struct{})
	var later, reports atomic.Int64
	if err := registry.Install(&Extension{Name: "hooks", TaskHooks: []TaskHookRegistration{{Task: "task.hook-cancel", Handlers: map[string]TaskHook{"ping": func(ctx context.Context, _ any) (any, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }}}, {Task: "task.hook-cancel", Handlers: map[string]TaskHook{"ping": func(context.Context, any) (any, error) { later.Add(1); return nil, nil }}}}}); err != nil {
		t.Fatal(err)
	}
	definition := taskDefinition(t, "task.hook-cancel", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.EachHook(ctx, "ping", func(handler TaskHook) error { _, err := handler(ctx, nil); return err })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry, OnReport: func(error) { reports.Add(1) }})
	conv := root(t, h, ModelRef{goai.ProviderOpenAI, "model"})
	var id ID
	_, err := h.CommitTasks(bg, conv.ID(), func(tx *Tx) error {
		var err error
		id, err = tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	if _, err := h.AbortTask(bg, id); err != nil {
		t.Fatal(err)
	}
	record, err := h.WaitForTask(bg, id)
	if err != nil || record.State.Outcome.Status != "aborted" || later.Load() != 0 || reports.Load() != 0 {
		t.Fatal(record, err, later.Load(), reports.Load())
	}
}

func TestTaskHooksInvokePanicAndInvalidRegistrationIsolation(t *testing.T) {
	registry := NewRegistry()
	for _, hook := range []TaskHookRegistration{{Task: "", Handlers: map[string]TaskHook{"ping": func(context.Context, any) (any, error) { return nil, nil }}}, {Task: "task.hooks", Handlers: map[string]TaskHook{"bad hook": func(context.Context, any) (any, error) { return nil, nil }}}, {Task: "task.hooks", Handlers: map[string]TaskHook{"ping": nil}}} {
		if err := registry.Install(&Extension{Name: "invalid", TaskHooks: []TaskHookRegistration{hook}}); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
	var reports atomic.Int64
	handler := func(context.Context, any) (any, error) { return nil, nil }
	if err := registry.Install(&Extension{Name: "hooks", TaskHooks: []TaskHookRegistration{{Task: "task.hooks", Handlers: map[string]TaskHook{"ping": handler}}, {Task: "task.hooks", Handlers: map[string]TaskHook{"ping": handler}}}}); err != nil {
		t.Fatal(err)
	}
	definition := taskDefinition(t, "task.hooks", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		calls := 0
		if err := r.EachHook(ctx, "ping", func(TaskHook) error {
			calls++
			if calls == 1 {
				panic("private callback")
			}
			return errors.New("ordinary callback")
		}); err != nil {
			return err
		}
		if calls != 2 {
			t.Error(calls)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry, OnReport: func(error) { reports.Add(1) }})
	conv := root(t, h, ModelRef{goai.ProviderOpenAI, "model"})
	var id ID
	_, err := h.CommitTasks(bg, conv.ID(), func(tx *Tx) error {
		var err error
		id, err = tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := h.WaitForTask(bg, id)
	if err != nil || record.State.Outcome.Status != "completed" || reports.Load() != 2 {
		t.Fatal(record, err, reports.Load())
	}
}
