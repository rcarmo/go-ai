package durable

import (
	"context"
	"errors"
	"testing"
)

func TestTaskAgentCancelledOnlyCallerFailureCachedUntilNextPhase(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		gate, waiting := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, gate)
		var harness *Harness
		var documentID ID
		definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.agent-failure", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil }, Phases: map[string]TaskPhase{
			"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				caller, cancel := context.WithCancel(ctx)
				cancel()
				if _, err := r.Agent(caller); !errors.Is(err, context.Canceled) {
					t.Error("cancelled caller", err)
				}
				close(waiting)
				<-gate
				r.phaseMu.RLock()
				resolution := r.agentResolution
				r.phaseMu.RUnlock()
				<-resolution.done
				if resolution.err == nil {
					t.Error("resolution expected malformed agent")
				}
				_, first := r.Agent(ctx)
				if first == nil {
					t.Error("failed resolution disappeared")
				}
				// Restore the agent while this phase remains active. Its cached error must
				// survive; phase b must read the restored document independently.
				if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					handle, err := tx.Document(documentID)
					if err != nil {
						return nil, err
					}
					return nil, handle.Set(JSON{"model": map[string]any{"provider": "openai", "id": "model"}, "name": "restored", "systemPrompt": "", "settings": map[string]any{}})
				}); err != nil {
					return err
				}
				_, second := r.Agent(ctx)
				if second != first {
					t.Error("failure not cached", first, second)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
				})
			},
			"b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				agent, err := r.Agent(ctx)
				if err != nil {
					return err
				}
				if agent.Configuration.Name != "restored" {
					t.Error(agent)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		registry := NewRegistry()
		if _, err := registry.RegisterTask(definition); err != nil {
			t.Fatal(err)
		}
		harness = openHarness(t, b.store, Options{Registry: registry})
		var id ID
		_, err = harness.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			documentID, err = tx.MintID()
			if err != nil {
				return err
			}
			if _, err := tx.CreateDocument(Document{ID: documentID, Scope: "conversation", Owner: 1, Kind: "pi.agent", Version: 1, History: "rewindable", Fork: "asOf", Value: JSON{"settings": "invalid settings shape"}}); err != nil {
				return err
			}
			id, err = tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := harness.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, waiting)
		close(gate)
		record, err := harness.WaitForTask(bg, id)
		if err != nil || record.State.Outcome == nil || record.State.Outcome.Status != "completed" {
			t.Fatal(record, err)
		}
	})
}
