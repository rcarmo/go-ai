package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

type revokeSandbox120 struct {
	sandbox *QuickJSSandbox
	starts  atomic.Int64
	ready   chan struct{}
}

func (s *revokeSandbox120) RuntimeIdentity() string { return s.sandbox.RuntimeIdentity() }
func (s *revokeSandbox120) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	vm, err := s.sandbox.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	return &revokeExecution120{CodemodeExecution: vm, first: s.starts.Add(1) == 1, ready: s.ready}, nil
}

type revokeExecution120 struct {
	CodemodeExecution
	first bool
	ready chan struct{}
}

func (e *revokeExecution120) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if err == nil && event.Call != nil && event.Call.ID == 2 && e.first {
		close(e.ready)
		<-ctx.Done()
		return CodemodeEvent{}, ctx.Err()
	}
	return event, err
}
func TestCodemode120PaidReopenReplaysDeliveryButRevokesNewEffect(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		sandbox := &revokeSandbox120{sandbox: engine, ready: make(chan struct{})}
		var effects atomic.Int64
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "revoke-classifier-120", Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			effects.Add(1)
			return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{"yes": {Type: "bool", Probability: 1}}, StopReason: "stop"}, nil
		}})
		registry := NewRegistry()
		def := taskDefinition(t, "paid-revoke-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const model={provider:"test",id:"judge"};const c={state:{},questions:{yes:{type:"bool",instructions:"yes",criteria:{true:"yes",false:"no"}}}};const first=await models.classify(model,c);if(first.answers.yes.probability!==1)throw Error("lost delivery");let revoked=false;try{await models.classify(model,c)}catch(e){revoked=true}return revoked;`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != true {
				t.Error("revocation result", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "test", ID: "judge", Api: "revoke-classifier-120"}}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		if effects.Load() != 1 {
			t.Fatal("first effect", effects.Load())
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		options := h.options
		options.PaidModels = false
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("%+v %+v effects=%d starts=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), sandbox.starts.Load())
		}
	})
}
