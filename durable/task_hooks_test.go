package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTaskHooksPinnedPhaseOrderingErrorsDetachmentAndEnded(t *testing.T) {
	registry := NewRegistry()
	var reports atomic.Int64
	seen := []string{}
	var escaped TaskHook
	handlers := map[string]TaskHook{"ping": func(_ context.Context, input any) (any, error) {
		input.(map[string]any)["value"] = "changed"
		return "first", nil
	}}
	for _, extension := range []*Extension{{Name: "first", TaskHooks: []TaskHookRegistration{{Task: "task.custom-hooks", Handlers: handlers}}}, {Name: "broken", TaskHooks: []TaskHookRegistration{{Task: "task.custom-hooks", Handlers: map[string]TaskHook{"ping": func(context.Context, any) (any, error) { return nil, errors.New("host failure") }}}}}, {Name: "panic", TaskHooks: []TaskHookRegistration{{Task: "task.custom-hooks", Handlers: map[string]TaskHook{"ping": func(context.Context, any) (any, error) { panic("private panic") }}}}}} {
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
	}
	before, release := make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, release)
	each := func(ctx context.Context, r *TaskRuntime) error {
		return r.EachHook(ctx, "ping", func(handler TaskHook) error {
			escaped = handler
			input := map[string]any{"value": "original"}
			value, err := handler(ctx, input)
			if input["value"] != "original" {
				t.Error("hook input alias")
			}
			if err == nil {
				seen = append(seen, value.(string))
			}
			return err
		})
	}
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.custom-hooks", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil }, Phases: map[string]TaskPhase{
		"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := each(ctx, r); err != nil {
				return err
			}
			close(before)
			<-release
			if err := each(ctx, r); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
			})
		},
		"b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := each(ctx, r); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry, OnReport: func(error) { reports.Add(1) }})
	conv := root(t, h, ModelRef{goai.ProviderOpenAI, "model"})
	var id ID
	_, err = h.CommitTasks(bg, conv.ID(), func(tx *Tx) error {
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
	awaitTaskSignal(t, before)
	handlers["ping"] = func(context.Context, any) (any, error) { t.Error("mutated installed hook"); return "mutated", nil }
	if err := registry.Install(&Extension{Name: "late", TaskHooks: []TaskHookRegistration{{Task: "task.custom-hooks", Handlers: map[string]TaskHook{"ping": func(context.Context, any) (any, error) { return "late", nil }}}}}); err != nil {
		t.Fatal(err)
	}
	selected := []string{"late", "first"}
	if err := conv.ConfigurePatch(bg, AgentPatch{Extensions: &selected}); err != nil {
		t.Fatal(err)
	}
	close(release)
	record, err := h.WaitForTask(bg, id)
	if err != nil || record.State.Outcome.Status != "completed" {
		t.Fatal(record, err)
	}
	if strings.Join(seen, ",") != "first,first,late,first" || reports.Load() != 4 {
		t.Fatal(seen, reports.Load())
	}
	// A captured handler must lose authority at actual invocation return.
	var returned chan struct{}
	h.session.taskBookkeeping(func() {
		if r := h.scheduler.invocations[id]; r != nil {
			returned = r.done
		}
	})
	if returned != nil {
		awaitTaskSignal(t, returned)
	}
	if _, err := escaped(bg, nil); !errors.Is(err, ErrSealed) {
		t.Fatal("escaped hook", err)
	}
}
