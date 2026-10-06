package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestConversationCreatedHelpersSeeInheritedAgentBeforeOverrideAndNilRawCompatibility(t *testing.T) {
	registry := NewRegistry()
	var callbacks atomic.Int64
	options := Options{Registry: registry, ConversationCreated: func(tx *Tx, conversation Conversation) error {
		callbacks.Add(1)
		candidate, err := tx.current()
		if err != nil {
			return err
		}
		doc, ok := agentDocument(candidate, conversation.ID)
		if !ok {
			return reject("missing created agent")
		}
		if conversation.Owner != 0 && doc.Value["cwd"] != "/root" {
			t.Error("helper override preceded creation", doc.Value)
		}
		return nil
	}}
	h := openHarness(t, mustMemory(t), options)
	root, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/root"})
	if err != nil {
		t.Fatal(err)
	}
	definition := taskDefinition(t, "task.created-helper", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		override := AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/foreground"}
		child, err := r.CreateOwnedConversation(ctx, "helper", &override)
		if err != nil {
			return err
		}
		value, err := child.Agent(ctx)
		if err != nil || value.Configuration.Cwd != "/foreground" {
			t.Error(value, err)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	id := createPublicTask(t, h, definition, nil, TaskOptions{Conversation: root.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
	waitPublicTask(t, h, id)
	child, err := root.CreateBackgroundConversation(bg, "helper-bg", &AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/background"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := child.Agent(bg)
	if err != nil || value.Configuration.Cwd != "/background" || callbacks.Load() != 3 {
		t.Fatal(value, err, callbacks.Load())
	}
	// Harness-owned transactions initialize documents even without a callback.
	plain := taskTestHarness(t, mustMemory(t))
	var raw ID
	_, err = plain.CommitTasks(bg, 1, func(tx *Tx) error {
		var err error
		raw, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.CreateConversation(Conversation{ID: raw})
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := plain.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := agentDocument(snapshot, raw); !ok {
		t.Fatal("nil callback suppressed harness documents")
	}
}

func TestHarnessRawCreationWithoutCallbackInitializesButPlainSessionAgentReadDoesNot(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		var plain ID
		_, err = session.Commit(bg, func(tx *Tx) error {
			var err error
			plain, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: plain})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := session.Close(bg); err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store))
		handle, err := h.Conversation(bg, plain)
		if err != nil {
			t.Fatal(err)
		}
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		agent, err := handle.Agent(bg)
		if err != nil || agent.Configuration.ThinkingLevel != "off" || agent.Configuration.Model.ID != "" {
			t.Fatal("absent agent getter", agent, err)
		}
		contextView, err := handle.Context(bg)
		if err != nil || len(contextView.Messages) != 0 {
			t.Fatal("absent agent context", contextView, err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || before.Seq != after.Seq || len(before.Documents) != len(after.Documents) {
			t.Fatal("agent getter wrote", before.Seq, after.Seq, err)
		}
		level := goai.ModelThinkingLevel("low")
		if err := handle.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &level}); err != nil {
			t.Fatal("configure absent agent", err)
		}
		agent, err = handle.Agent(bg)
		if err != nil || agent.Configuration.ThinkingLevel != "low" {
			t.Fatal(agent, err)
		}
		var created ID
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			created, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: created})
		})
		if err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]bool{}
		for _, doc := range state.Documents {
			if doc.Scope == "conversation" && doc.Owner == created && !doc.Retired {
				kinds[doc.Kind] = true
			}
		}
		for _, kind := range []string{"pi.agent", "pi.inbox", "pi.usage", "pi.live"} {
			if !kinds[kind] {
				t.Fatal("nil callback missing builtin", kind)
			}
		}
	})
}
