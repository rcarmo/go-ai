package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestTaskEnvironmentPinnedPerUseCurrentCwdOffLineAndEnded(t *testing.T) {
	registry := NewRegistry()
	before, release := make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, release)
	var factories atomic.Int64
	var escaped *TaskRuntime
	definition := taskDefinition(t, "task.env-use", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		escaped = r
		first, err := r.Environment(ctx)
		if err != nil {
			return err
		}
		if first.Cwd() != "/one" {
			t.Error(first.Cwd())
		}
		close(before)
		<-release
		second, err := r.Environment(ctx)
		if err != nil {
			return err
		}
		if second.Cwd() != "/two" {
			t.Error(second.Cwd())
		}
		caller, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := r.Environment(caller); !errors.Is(err, context.Canceled) {
			t.Error(err)
		}
		// Deliberately pass the zero interface as invalid API input.
		var absentContext context.Context
		if _, err := r.Environment(absentContext); err == nil {
			t.Error("nil context accepted")
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	options := Options{Registry: registry, Env: func(ctx context.Context, target EnvTarget) (ExecutionEnvironment, error) {
		factories.Add(1)
		if _, err := target.Read.ContextView(ctx, target.Conversation, 0); err != nil {
			return nil, err
		}
		return recoveryEnvironment(target.Cwd), nil
	}}
	h := openHarness(t, mustMemory(t), options)
	conv := root(t, h, ModelRef{goai.ProviderOpenAI, "model"})
	cwd := "/one"
	if err := conv.ConfigurePatch(bg, AgentPatch{Cwd: &cwd}); err != nil {
		t.Fatal(err)
	}
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
	awaitTaskSignal(t, before)
	cwd = "/two"
	if err := conv.ConfigurePatch(bg, AgentPatch{Cwd: &cwd}); err != nil {
		t.Fatal(err)
	}
	close(release)
	record, err := h.WaitForTask(bg, id)
	if err != nil || record.State.Outcome.Status != "completed" || factories.Load() != 2 {
		t.Fatal(record, err, factories.Load())
	}
	awaitTaskSignal(t, escaped.done)
	if _, err := escaped.Environment(bg); !errors.Is(err, ErrSealed) {
		t.Fatal(err)
	}
}

func TestTaskEnvironmentFactoryPanicFailureAndAbsent(t *testing.T) {
	for _, mode := range []string{"panic", "error", "absent"} {
		t.Run(mode, func(t *testing.T) {
			registry := NewRegistry()
			definition := taskDefinition(t, "task.env-failure", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				env, err := r.Environment(ctx)
				if mode == "absent" {
					if env != nil || err != nil {
						t.Error(env, err)
					}
				} else if err == nil {
					t.Error("factory failure hidden")
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
			})
			if _, err := registry.RegisterTask(definition); err != nil {
				t.Fatal(err)
			}
			options := Options{Registry: registry}
			if mode != "absent" {
				options.Env = func(context.Context, EnvTarget) (ExecutionEnvironment, error) {
					if mode == "panic" {
						panic("private factory panic")
					}
					return nil, errors.New("factory")
				}
			}
			h := openHarness(t, mustMemory(t), options)
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
			if _, err := h.WaitForTask(bg, id); err != nil {
				t.Fatal(err)
			}
		})
	}
}
