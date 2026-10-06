package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestTaskAgentPinnedLazyPhaseSnapshotRefreshDetachedAndEnded(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "base", Tools: []ToolRegistration{wrapRegistration("base")}}); err != nil {
		t.Fatal(err)
	}
	beforeUse, afterUse, waitingBefore, waitingAfter := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, beforeUse, afterUse)
	var escaped *TaskRuntime
	seen := []string{}
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.agent-phases", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil }, Phases: map[string]TaskPhase{
		"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			escaped = r
			close(waitingBefore)
			<-beforeUse
			first, err := r.Agent(ctx)
			if err != nil {
				return err
			}
			seen = append(seen, string(first.Configuration.ThinkingLevel)+":"+first.Tools[0].Name)
			first.Tools[0].Parameters[0] = 'x'
			first.Extensions[0] = "mutated"
			close(waitingAfter)
			<-afterUse
			second, err := r.Agent(ctx)
			if err != nil {
				return err
			}
			if len(second.Tools) != 1 || second.Tools[0].Name != "base" || !json.Valid(second.Tools[0].Parameters) || second.Extensions[0] != "base" {
				t.Error("phase snapshot/detachment", second)
			}
			seen = append(seen, string(second.Configuration.ThinkingLevel)+":"+second.Tools[0].Name)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
			})
		},
		"b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			agent, err := r.Agent(ctx)
			if err != nil {
				return err
			}
			names := []string{}
			for _, tool := range agent.Tools {
				names = append(names, tool.Name)
			}
			seen = append(seen, string(agent.Configuration.ThinkingLevel)+":"+strings.Join(names, ","))
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry})
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
	awaitTaskSignal(t, waitingBefore)
	level := goai.ModelThinkingLevel("low")
	if err := conv.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &level}); err != nil {
		t.Fatal(err)
	}
	close(beforeUse)
	awaitTaskSignal(t, waitingAfter)
	if err := registry.Install(&Extension{Name: "late", Tools: []ToolRegistration{wrapRegistration("late")}}); err != nil {
		t.Fatal(err)
	}
	level = "high"
	if err := conv.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &level}); err != nil {
		t.Fatal(err)
	}
	close(afterUse)
	if _, err := h.WaitForTask(bg, id); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "low:base|low:base|high:base,late" {
		t.Fatal(seen)
	}
	awaitTaskSignal(t, escaped.done)
	if _, err := escaped.Agent(bg); !errors.Is(err, ErrSealed) {
		t.Fatal("ended agent", err)
	}
}

func TestTaskAgentCancelledCallerThenIndependentSuccess(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "base", Tools: []ToolRegistration{wrapRegistration("base")}}); err != nil {
		t.Fatal(err)
	}
	definition := taskDefinition(t, "task.agent-caller", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		caller, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := r.Agent(caller); !errors.Is(err, context.Canceled) {
			return err
		}
		agent, err := r.Agent(ctx)
		if err != nil {
			return err
		}
		if len(agent.Tools) != 1 || agent.Tools[0].Name != "base" {
			t.Error(agent)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry})
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
	if err != nil || record.State.Outcome.Status != "completed" {
		t.Fatal(record, err)
	}
}

func mustMemory(t *testing.T) *MemoryStorage {
	t.Helper()
	store, err := NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestTaskAgentPinnedCancelledCallerKeepsSharedResolutionAndCloseJoins(t *testing.T) {
	registry := NewRegistry()
	entered, release := make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, release)
	if err := registry.Install(&Extension{Name: "base", Tools: []ToolRegistration{wrapRegistration("base")}, Wraps: []ExtensionWrap{{Tool: "base", WrapTool: func(reg ToolRegistration) (ToolRegistration, error) { close(entered); <-release; return reg, nil }}}}); err != nil {
		t.Fatal(err)
	}
	waiting, allowReturn := make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, allowReturn)
	var runtime *TaskRuntime
	definition := taskDefinition(t, "task.agent-cancel", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		runtime = r
		caller, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := r.Agent(caller); !errors.Is(err, context.Canceled) {
			t.Error("caller cancellation", err)
		}
		close(waiting)
		<-allowReturn
		return ctx.Err()
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry})
	conv := root(t, h, ModelRef{goai.ProviderOpenAI, "model"})
	_, err := h.CommitTasks(bg, conv.ID(), func(tx *Tx) error {
		_, err := tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, waiting)
	awaitTaskSignal(t, entered)
	closed := make(chan error, 1)
	go func() { closed <- h.Close(bg) }()
	<-h.life.Done()
	close(allowReturn)
	select {
	case <-runtime.done:
		t.Fatal("resolution host abandoned")
	default:
	}
	select {
	case err := <-closed:
		t.Fatal("Close returned before wrapper", err)
	default:
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, runtime.done)
}
