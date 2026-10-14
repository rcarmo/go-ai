package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120ClassifierProviderFailureAndInvalidContext(t *testing.T) {
	api := goai.ClassifierApi("codemode-errors-" + t.Name())
	var effects atomic.Int64
	goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
		effects.Add(1)
		return &goai.ClassifierResult{StopReason: goai.StopReasonError, ErrorMessage: "quota exceeded", Answers: map[string]goai.ClassifierAnswer{}}, nil
	}})
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		effects.Store(0)
		registry := NewRegistry()
		def := taskDefinition(t, "classifier-error", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			// Invalid calls never reach the classifier; the script still receives a catchable Error.
			for i, code := range []string{`await models.classify({provider:"test",id:"judge"},{state:[],questions:{q:{type:"bool",instructions:"q",criteria:{true:"yes",false:"no"}}}});`, `await models.classify({provider:"test",id:"judge"},{state:{},questions:{}});`, `await models.classify({provider:"test",id:"judge"},{state:{},questions:{q:{type:"bogus",instructions:"q"}}});`} {
				result, err := r.RunCodemode(ctx, string(rune('a'+i)), `try{`+code+`throw Error("invalid accepted")}catch(e){text(e.message);}`, sandbox)
				if err != nil {
					return err
				}
				if result.Error != "" || result.Output == "" || strings.Contains(result.Output, "invalid accepted") {
					t.Errorf("invalid context %+v", result)
				}
			}
			if effects.Load() != 0 {
				t.Error("invalid context dispatched", effects.Load())
			}
			code := `const r=await models.classify({provider:"test",id:"judge"},{state:{},questions:{q:{type:"bool",instructions:"q",criteria:{true:"yes",false:"no"}}}});if(r.stopReason!=="error"||r.errorMessage!=="quota exceeded")throw Error("provider failure rejected/lost");return "ok";`
			for i := 0; i < 2; i++ {
				result, err := r.RunCodemode(ctx, "provider-error", code, sandbox)
				if err != nil {
					return err
				}
				if result.Error != "" || result.Value != "ok" {
					t.Errorf("provider failure %+v", result)
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{ID: "judge", Provider: "test", Api: api}}})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 {
			t.Fatalf("%+v error=%+v effects=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load())
		}
	})
}
