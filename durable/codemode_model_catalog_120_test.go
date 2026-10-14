package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCodemode120QuickJSModelCatalogScopedSanitizedAndReuse(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "scoped", ID: "judge", Type: "classifier", BaseURL: "https://private.invalid", Headers: map[string]string{"Authorization": "private"}}}
		registry := NewRegistry()
		def := taskDefinition(t, "catalog-global-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := `const all=await models.getModelsOfType("classifier","scoped");if(all.length!==1||all[0].id!=="judge"||all[0].headers||all[0].baseUrl)throw Error("scope leak");const one=await models.getModelOfType("classifier","scoped","judge");if(one.id!=="judge")throw Error("lookup");const absent=await models.getModelOfType("classifier","other","judge");if(absent!==null)throw Error("fallback");let unavailable=false;try{await models.getAvailableOfType("classifier","scoped")}catch(e){unavailable=true}if(!unavailable)throw Error("unsupported hidden");return one.id;`
			result, err := r.RunCodemode(ctx, "run", code, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != "judge" {
				t.Error(result)
			}
			catalog.classifier = nil
			if _, err = r.RunCodemode(ctx, "run", code, sandbox); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
