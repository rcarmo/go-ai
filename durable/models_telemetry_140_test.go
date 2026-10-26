package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func TestModels140PaidTelemetryInheritanceAndOverride(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "inherit"
		if explicit {
			name = "override"
		}
		t.Run(name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
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
