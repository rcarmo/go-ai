package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSClassifierGlobalOptInAndReplay(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		var effects atomic.Int64
		api := goai.ClassifierApi("codemode-classifier-120")
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			effects.Add(1)
			return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{"yes": {Type: "bool", Probability: 1}}, StopReason: goai.StopReasonStop}, nil
		}})
		registry := NewRegistry()
		def := taskDefinition(t, "model-global-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := `if(ALL_TOOLS.length!==0)throw Error("hidden model tools");let rejected=false;try{await models.classify(null,{})}catch(e){rejected=true}if(!rejected)throw Error("invalid model");const result=await models.classify({provider:"test",id:"judge"},{state:{},questions:{yes:{type:"bool",instructions:"yes?",criteria:{true:"yes",false:"no"}}}});return result.answers.yes.probability;`
			result, err := r.RunCodemode(ctx, "run", code, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || !equalJSONValue(result.Value, 1) {
				t.Errorf("model global %+v", result)
			}
			if _, err = r.RunCodemode(ctx, "run", code, sandbox); err != nil {
				return err
			}
			if effects.Load() != 1 {
				t.Error("paid effect repeated", effects.Load())
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "test", ID: "judge", Api: api}}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
