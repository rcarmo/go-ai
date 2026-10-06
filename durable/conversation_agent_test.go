package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestConversationAgentCurrentRegistryDetachedNoScheduleAndInvocationFence(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "base", Tools: []ToolRegistration{wrapRegistration("base")}}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, mustMemory(t), Options{Registry: registry})
	conv := root(t, h, ModelRef{goai.ProviderOpenAI, "model"})
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	first, err := conv.Agent(bg)
	if err != nil || len(first.Tools) != 1 {
		t.Fatal(first, err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatal("agent read wrote", after.Seq, before.Seq, err)
	}
	first.Tools[0].Name = "mutated"
	first.Tools[0].Parameters[0] = 'x'
	first.Extensions[0] = "mutated"
	if err := registry.Install(&Extension{Name: "late", Tools: []ToolRegistration{wrapRegistration("late")}}); err != nil {
		t.Fatal(err)
	}
	name := "new"
	if err := conv.ConfigurePatch(bg, AgentPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	inspectionBefore, err := h.InspectTasks(bg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := conv.Agent(bg)
	if err != nil || second.Configuration.Name != "new" || len(second.Tools) != 2 || second.Tools[0].Name != "base" || second.Extensions[0] != "base" {
		t.Fatal(second, err)
	}
	inspection, err := h.InspectTasks(bg)
	if err != nil || inspection.Scheduling != inspectionBefore.Scheduling {
		t.Fatal("agent read scheduled", inspection, err)
	}
	var escaped *InvocationConversation
	definition := taskDefinition(t, "task.agent-handle", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		bound, err := r.Conversation(ctx, conv.ID())
		if err != nil {
			return err
		}
		escaped = bound
		value, err := bound.Agent(ctx)
		if err != nil {
			return err
		}
		if value.Configuration.Name != "new" {
			t.Error(value)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	var id ID
	_, err = h.CommitTasks(bg, conv.ID(), func(tx *Tx) error {
		var err error
		id, err = tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WaitForTask(bg, id); err != nil {
		t.Fatal(err)
	}
	var done chan struct{}
	h.session.taskBookkeeping(func() {
		if r := h.scheduler.invocations[id]; r != nil {
			done = r.done
		}
	})
	if done != nil {
		awaitTaskSignal(t, done)
	}
	if _, err := escaped.Agent(bg); !errors.Is(err, ErrSealed) {
		t.Fatal("escaped agent handle", err)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := conv.Agent(bg); !errors.Is(err, ErrClosed) {
		t.Fatal("closed agent", err)
	}
}
