package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestPlainSessionAbsentAgentEnvironmentAndNoModelSubmit(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		session, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		var conversation ID
		_, err = session.Commit(bg, func(tx *Tx) error {
			var err error
			conversation, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: conversation})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := session.Close(bg); err != nil {
			t.Fatal(err)
		}
		var envCalls atomic.Int64
		definition := taskDefinition(t, "task.absent-agent-default", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			agent, err := r.Agent(ctx)
			if err != nil {
				return err
			}
			if agent.Configuration.ThinkingLevel != "off" {
				t.Error(agent)
			}
			if _, err := r.Environment(ctx); err != nil {
				return err
			}
			owned, err := r.CreateOwnedConversation(ctx, "child", nil)
			if err != nil {
				return err
			}
			agent, err = owned.Agent(ctx)
			if err != nil {
				return err
			}
			if agent.Configuration.Model.ID != "" {
				t.Error(agent)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("defaults"), nil })
		})
		h := taskTestHarnessOptions(t, reopenStoreAfterHarnessClose(t, b.store), Options{Env: func(_ context.Context, target EnvTarget) (ExecutionEnvironment, error) {
			envCalls.Add(1)
			if target.Cwd != "" {
				t.Error("unexpected default cwd", target.Cwd)
			}
			return nil, nil
		}}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "conversation"}})
		if final := waitPublicTask(t, h, id); final.State.Outcome.Status != "completed" || envCalls.Load() != 1 {
			t.Fatal(final, envCalls.Load())
		}
		handle, err := h.Conversation(bg, conversation)
		if err != nil {
			t.Fatal(err)
		}
		sub, err := handle.Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		final := waitSubmission(t, sub)
		if final.Submission.Status != "failed" || final.Submission.Value["errorCode"] != "no_model" {
			t.Fatal(final)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := agentDocument(state, conversation); exists {
			t.Fatal("defaults wrote optional agent")
		}
	})
}
