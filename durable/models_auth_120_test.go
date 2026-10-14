package durable

import (
	"context"
	"errors"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type authCatalog120 struct {
	*typedCatalog120
	widen bool
	fail  error
}

func (c *authCatalog120) HasModelAuth(ctx context.Context, ref goai.TypedModelRef) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.fail != nil {
		return false, c.fail
	}
	return ref.ID == "image", nil
}
func (c *authCatalog120) GetAvailableModels(ctx context.Context, kind goai.ModelType, provider goai.Provider) ([]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.fail != nil {
		return nil, c.fail
	}
	if c.widen {
		return []any{&goai.ImageModel{Provider: "other", ID: "outside"}}, nil
	}
	return c.GetTypedModels(kind, provider), nil
}
func TestModels120UnconfiguredAuthCapabilityDoesNotGuess(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		catalog := goai.NewModelRuntime(nil)
		catalog.SetProvider(goai.StaticModelProvider{Provider: "scoped", Models: []*goai.Model{{Provider: "scoped", ID: "chat"}}})
		def := taskDefinition(t, "unsupported-auth", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			if _, e = access.HasAuth(ctx, goai.TypedModelRef{Provider: "scoped", ID: "chat"}); !errors.Is(e, ErrUnsupported) {
				t.Fatal("guessed auth", e)
			}
			if _, e = access.GetAvailable(ctx, "scoped"); !errors.Is(e, ErrUnsupported) {
				t.Fatal(e)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		registry := NewRegistry()
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
	})
}

func TestModels120AuthScopeDetachmentErrorsAndLifetime(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		catalog := &authCatalog120{typedCatalog120: &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image", BaseURL: "private", Headers: map[string]string{"Authorization": "secret"}}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier"}}}
		var escaped *ModelAccess
		def := taskDefinition(t, "auth-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			escaped = access
			ok, e := access.HasAuth(ctx, goai.TypedModelRef{Type: goai.ModelTypeImage, Provider: "scoped", ID: "image"})
			if e != nil || !ok {
				t.Fatal(ok, e)
			}
			ok, e = access.HasAuth(ctx, goai.TypedModelRef{Type: goai.ModelTypeImage, Provider: "other", ID: "image"})
			if e != nil || ok {
				t.Fatal("auth scope widened", ok, e)
			}
			models, e := access.GetAvailableOfType(ctx, goai.ModelTypeImage, "scoped")
			if e != nil || len(models) != 1 {
				t.Fatal(models, e)
			}
			image := models[0].(*goai.ImageModel)
			if image.BaseURL != "" || image.Headers != nil {
				t.Fatal("availability credentials leaked")
			}
			image.ID = "mutated"
			if catalog.image.ID != "image" {
				t.Fatal("availability alias")
			}
			catalog.widen = true
			if _, e = access.GetAvailableOfType(ctx, goai.ModelTypeImage, ""); e == nil {
				t.Fatal("availability expanded scoped catalogue")
			}
			catalog.widen = false
			cause := errors.New("auth unavailable")
			catalog.fail = cause
			if _, e = access.HasAuth(ctx, goai.TypedModelRef{Type: goai.ModelTypeImage, Provider: "scoped", ID: "image"}); !errors.Is(e, cause) {
				t.Fatal(e)
			}
			catalog.fail = nil
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if _, e = access.GetAvailableOfType(cancelled, goai.ModelTypeImage, ""); !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		registry := NewRegistry()
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		if _, e := escaped.GetAvailable(bg, ""); !errors.Is(e, ErrSealed) {
			t.Fatal("ended auth authority", e)
		}
	})
}
