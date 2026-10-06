package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationResponseHookOwnedWorkSettlesAtDecisionForSuccessAndFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failed], func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, release := make(chan struct{}), make(chan struct{})
				child := taskDefinition(t, "task.response-hook.owned", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(entered)
					<-release
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
				})
				registry := NewRegistry()
				if _, err := registry.RegisterTask(child); err != nil {
					t.Fatal(err)
				}
				var owner ID
				if err := registry.Install(&Extension{Name: "response-owned", Hooks: GenerationHooks{AfterResponse: func(ctx context.Context, _ MessageReceipt, api *HookAPI) error {
					owner = api.TaskID()
					if err := api.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						_, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
						return nil, err
					}); err != nil {
						return err
					}
					awaitTaskSignal(t, entered)
					return nil
				}}}); err != nil {
					t.Fatal(err)
				}
				var requests atomic.Int64
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					requests.Add(1)
					ch := make(chan goai.Event, 1)
					message := terminal("answer")
					if failed {
						message.Reason = goai.StopReasonError
						message.Message.StopReason = goai.StopReasonError
						message.Message.ErrorMessage = "billing limit reached"
					}
					ch <- message
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, release)
				sub, err := root(t, h, ref).Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				settled := waitSubmission(t, sub)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				record, err := CanonicalTask(state.Tasks[owner], h.session.limits)
				if err != nil || record.State.Status != "completing" {
					t.Fatal("response-owned work did not hold task", record, err)
				}
				want := "completed"
				if failed {
					want = "failed"
				}
				if record.State.Outcome.Status != want || requests.Load() != 1 {
					t.Fatal(record, requests.Load())
				}
				if failed {
					if settled.Record.Status != "unanswered" || settled.Record.Reason != "model_error" || settled.Record.Answer != 0 {
						t.Fatal("held model-error settlement", settled)
					}
				} else if settled.Record.Status != "done" || settled.Record.Answer == 0 {
					t.Fatal("held answer settlement", settled)
				}
				releaseTaskGate(release)
				waitPublicTask(t, h, owner)
				final, err := h.Snapshot(bg)
				if err != nil || requests.Load() != 1 {
					t.Fatal(final, err, requests.Load())
				}
				assertTaskModelSpend(t, final, 1, ref.Provider, ref.ID, 5)
			})
		})
	}
}
