package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestNestedTool120EnvironmentResolvedBeforeExecutionOnce(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "ok"
		if fail {
			name = "fail"
		}
		t.Run(name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var environments, runs atomic.Int64
				registry := NewRegistry()
				reg := wrapRegistration("env_nested")
				reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					runs.Add(1)
					first, e := api.Environment(ctx)
					if e != nil {
						return ToolResult{}, e
					}
					second, e := api.Environment(ctx)
					if e != nil {
						return ToolResult{}, e
					}
					if first.Cwd() != "/nested" || second.Cwd() != first.Cwd() {
						t.Fatal(first, second)
					}
					return ToolResult{Content: "ok"}, nil
				}
				if e := registry.Register(reg); e != nil {
					t.Fatal(e)
				}
				def := taskDefinition(t, "env-nested-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					result, e := r.CallToolAuto(ctx, "env_nested", JSON{})
					if e != nil {
						return e
					}
					if fail && !result.IsError || !fail && result.Content != "ok" {
						t.Fatal(result)
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry, Env: func(_ context.Context, target EnvTarget) (ExecutionEnvironment, error) {
					environments.Add(1)
					if fail {
						return nil, errors.New("private environment failure")
					}
					if target.Cwd != "/nested" {
						t.Error(target.Cwd)
					}
					return recoveryEnvironment(target.Cwd), nil
				}})
				conv := &ConversationHandle{h: h, id: 1}
				cwd := "/nested"
				if e := conv.ConfigurePatch(bg, AgentPatch{Cwd: &cwd}); e != nil {
					t.Fatal(e)
				}
				record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
				want := int64(1)
				if fail {
					want = 0
				}
				if record.State.Outcome.Status != "completed" || environments.Load() != 1 || runs.Load() != want {
					t.Fatalf("%+v %+v env:%d runs:%d", record.State.Outcome, record.State.Outcome.Error, environments.Load(), runs.Load())
				}
			})
		})
	}
}
