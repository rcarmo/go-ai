package durable

import (
	"context"
	"errors"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type typedCatalog120 struct {
	*goai.ModelRuntime
	image      *goai.ImageModel
	classifier *goai.ClassifierModel
}

func (c *typedCatalog120) GetTypedModel(ref goai.TypedModelRef) any {
	if ref.Type == goai.ModelTypeImage && ref.Provider == string(c.image.Provider) && ref.ID == c.image.ID {
		return c.image
	}
	if ref.Type == goai.ModelTypeClassifier && ref.Provider == string(c.classifier.Provider) && ref.ID == c.classifier.ID {
		return c.classifier
	}
	return nil
}
func (c *typedCatalog120) GetTypedModels(kind goai.ModelType, provider goai.Provider) []any {
	if kind == goai.ModelTypeImage && (provider == "" || string(provider) == string(c.image.Provider)) {
		return []any{c.image}
	}
	if kind == goai.ModelTypeClassifier && (provider == "" || string(provider) == string(c.classifier.Provider)) {
		return []any{c.classifier}
	}
	return nil
}
func TestModels120TypedScopedDetachedAndLifetime(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image", Name: "image", Type: "image", BaseURL: "private", Headers: map[string]string{"Authorization": "secret"}, Input: []string{"text"}}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier", Type: "classifier", BaseURL: "private", Headers: map[string]string{"Authorization": "secret"}}}
		var escaped *ModelAccess
		def := taskDefinition(t, "typed-models", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			escaped = access
			value, e := access.GetModelOfType(goai.ModelTypeImage, "scoped", "image")
			if e != nil {
				return e
			}
			image := value.(*goai.ImageModel)
			if image.BaseURL != "" || image.Headers != nil {
				t.Fatal("image credentials leaked")
			}
			image.Input[0] = "mutated"
			if catalog.image.Input[0] != "text" {
				t.Fatal("image alias")
			}
			values, e := access.GetModelsOfType(goai.ModelTypeClassifier, "scoped")
			if e != nil {
				return e
			}
			if len(values) != 1 || values[0].(*goai.ClassifierModel).Headers != nil || values[0].(*goai.ClassifierModel).BaseURL != "" {
				t.Fatal(values)
			}
			if value, e = access.GetModelOfType(goai.ModelTypeImage, "other", "image"); e != nil || value != nil {
				t.Fatal("scope widened", value, e)
			}
			if _, e = access.GetModelsOfType("unknown", ""); e == nil {
				t.Fatal("unknown model type")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Catalog: catalog}, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		if _, e := escaped.GetModelsOfType(goai.ModelTypeImage, ""); !errors.Is(e, ErrSealed) {
			t.Fatal("ended typed authority", e)
		}
	})
}
func TestModels120ChatOnlyScopeDoesNotFallBackToGlobalTypedRegistry(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		def := taskDefinition(t, "typed-empty-scope", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			values, e := access.GetModelsOfType(goai.ModelTypeImage, "")
			if e != nil || len(values) != 0 {
				t.Fatal("chat-only host scope widened", values, e)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Catalog: goai.NewModelRuntime(nil)}, def)
		waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
	})
}
