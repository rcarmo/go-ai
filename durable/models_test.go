package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"sync/atomic"
	"testing"
)

func TestModels110TaskHookCapabilityCatalogIsolationAndLifetime(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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

func TestModels140PaidTelemetryInheritanceAndOverride(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "inherit"
		if explicit {
			name = "override"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				phase := &goai.TelemetrySpan{Value: "phase"}
				phase.StartSpan = func(string, map[string]any) *goai.TelemetrySpan { return phase }
				override := &goai.TelemetrySpan{Value: "override"}
				want := "phase"
				if explicit {
					want = "override"
				}
				var calls atomic.Int64
				goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "telemetry-classifier-140", Classify: func(_ *goai.ClassifierModel, _ goai.ClassifierContext, opts *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
					if opts.Telemetry == nil || opts.Telemetry.Value != want {
						t.Error("classifier lost context", opts.Telemetry)
					}
					calls.Add(1)
					return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{"q": {Type: "bool", Probability: 1}}}, nil
				}})
				images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "telemetry-images-140", GenerateImages: func(_ *images.ImagesModel, _ images.ImagesContext, opts *images.ImagesOptions) (*images.AssistantImages, error) {
					if opts.Telemetry == nil || opts.Telemetry.Value != want {
						t.Error("images lost context", opts.Telemetry)
					}
					calls.Add(1)
					return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}, StopReason: "stop"}, nil
				}})
				defer images.UnregisterImagesApiProvider("telemetry-images-140")
				catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier", Api: "telemetry-classifier-140"}, image: &goai.ImageModel{Provider: "scoped", ID: "image", Api: "telemetry-images-140"}}
				registry := NewRegistry()
				def := taskDefinition(t, "task.paid-telemetry-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					models, err := r.Models()
					if err != nil {
						return err
					}
					input := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "bool", Instructions: "yes?", Criteria: goai.ClassifierBoolCriteria{True: "yes", False: "no"}}}}
					if _, err := models.Classify(ctx, "classify", goai.TypedModelRef{Provider: "scoped", ID: "classifier"}, input); err != nil {
						return err
					}
					if _, err := models.GenerateImages(ctx, "images", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}); err != nil {
						return err
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, err := registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				classifierOptions := &goai.ClassifierOptions{}
				imageOptions := &images.ImagesOptions{}
				if explicit {
					classifierOptions.Telemetry = override
					imageOptions.Telemetry = override
				}
				h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true, Telemetry: phase, ClassifierRequestOptions: func(context.Context, goai.TypedModelRef) (*goai.ClassifierOptions, error) {
					return classifierOptions, nil
				}, ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) { return imageOptions, nil }})
				root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				result := waitPublicTask(t, h, id)
				if result.State.Outcome == nil || result.State.Outcome.Status != "completed" || calls.Load() != 2 {
					t.Fatal(result, calls.Load())
				}
				if !explicit && (classifierOptions.Telemetry != nil || imageOptions.Telemetry != nil) {
					t.Fatal("mutated host options")
				}
			})
		})
	}
}
