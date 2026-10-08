package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestModels110TaskHookCapabilityCatalogIsolationAndLifetime(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		catalog := goai.NewModelRuntime(nil)
		catalog.SetProvider(goai.StaticModelProvider{Provider: "scoped", Models: []*goai.Model{{ID: "only", Provider: "scoped", Api: goai.ApiOpenAIResponses, Name: "Scoped", ContextWindow: 1000, MaxTokens: 100, BaseURL: "https://private.invalid", APIKey: "secret"}}})
		var access *ModelAccess
		definition := taskDefinition(t, "models-access", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			var e error
			access, e = r.Models()
			if e != nil {
				return e
			}
			models, e := access.GetModels("scoped")
			if e != nil {
				return e
			}
			if len(models) != 1 || models[0].ID != "only" || models[0].APIKey != "" || models[0].BaseURL != "" {
				t.Fatal("scoped catalog/credential leak", models)
			}
			models[0].Name = "mutated"
			m, e := access.GetModel("scoped", "only")
			if e != nil {
				return e
			}
			if m.Name != "Scoped" {
				t.Fatal("catalog alias", m)
			}
			if absent, e := access.GetModel("scoped", "absent"); e != nil || absent != nil {
				t.Fatal(absent, e)
			}
			if _, e = access.Refresh(ctx, goai.ModelRuntimeRefreshOptions{}); e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("ok"), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Catalog: catalog}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		if access == nil {
			t.Fatal("missing access")
		}
		if _, e := access.GetModels(""); !errors.Is(e, ErrSealed) {
			t.Fatal("ended capability remained live", e)
		}
	})
}
