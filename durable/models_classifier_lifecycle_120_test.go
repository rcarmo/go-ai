package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func paidClassifierInput120() goai.ClassifierContext {
	return goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "bool", Instructions: "decide", Criteria: goai.ClassifierBoolCriteria{True: "yes", False: "no"}}}}
}
func paidClassifierCatalog120(api goai.ClassifierApi) *typedCatalog120 {
	return &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image"}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier", Api: api}}
}
func TestModels120PaidClassifierUnsafeRestartAndProviderJoin(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		const api goai.ClassifierApi = "paid-classifier-restart"
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var calls, optionsCalls, attempts atomic.Int64
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(_ *goai.ClassifierModel, _ goai.ClassifierContext, options *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			calls.Add(1)
			close(entered)
			<-options.Context.Done()
			close(cancelled)
			<-release
			return nil, options.Context.Err()
		}})
		registry := NewRegistry()
		def := taskDefinition(t, "classifier-restart-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			_, e = access.Classify(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "classifier"}, paidClassifierInput120())
			if attempts.Add(1) == 1 {
				return e
			}
			if e == nil {
				return reject("interrupted classifier returned structured result")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidClassifierCatalog120(api), ClassifierRequestOptions: func(context.Context, goai.TypedModelRef) (*goai.ClassifierOptions, error) {
			optionsCalls.Add(1)
			return &goai.ClassifierOptions{APIKey: "host-key"}, nil
		}})
		cleanupTaskGates(t, release)
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, entered)
		done := make(chan error, 1)
		go func() { done <- h.Close(bg) }()
		awaitTaskSignal(t, cancelled)
		select {
		case e := <-done:
			t.Fatal("classifier close skipped host join", e)
		default:
		}
		releaseTaskGate(release)
		if e := watchReceive(t, done); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || calls.Load() != 1 || optionsCalls.Load() != 1 {
			t.Fatalf("%+v %+v calls:%d opts:%d", record.State.Outcome, record.State.Outcome.Error, calls.Load(), optionsCalls.Load())
		}
	})
}
func TestModels120PaidClassifierBilledErrorAndMalformedAnswer(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		name := "error"
		if malformed {
			name = "malformed"
		}
		t.Run(name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				const api goai.ClassifierApi = "paid-classifier-bill"
				var calls atomic.Int64
				goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
					calls.Add(1)
					answers := map[string]goai.ClassifierAnswer{}
					if malformed {
						answers["q"] = goai.ClassifierAnswer{Type: "unknown"}
					}
					return &goai.ClassifierResult{Answers: answers, StopReason: goai.StopReasonError, ErrorMessage: "classifier rejected", Usage: &goai.Usage{Input: 7, Output: 1, TotalTokens: 8}}, nil
				}})
				registry := NewRegistry()
				def := taskDefinition(t, "classifier-bill-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					access, e := r.Models()
					if e != nil {
						return e
					}
					for i := 0; i < 2; i++ {
						result, e := access.Classify(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "classifier"}, paidClassifierInput120())
						if e != nil {
							return e
						}
						if result.StopReason != goai.StopReasonError || result.Usage == nil || result.Usage.TotalTokens != 8 {
							t.Fatal(result)
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidClassifierCatalog120(api)})
				root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
				record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
				if record.State.Outcome.Status != "completed" {
					t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
				}
				usage, e := h.Usage(bg)
				if e != nil || usage.Tools[classifierToolName].TotalTokens != 8 || calls.Load() != 1 {
					t.Fatal("classifier billed error lost/duplicated", usage, e, calls.Load())
				}
				if e = h.Close(bg); e != nil {
					t.Fatal(e)
				}
				next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
				usage, e = next.Usage(bg)
				if e != nil || usage.Tools[classifierToolName].TotalTokens != 8 || calls.Load() != 1 {
					t.Fatal("classifier reopen bill", usage, e, calls.Load())
				}
			})
		})
	}
}
