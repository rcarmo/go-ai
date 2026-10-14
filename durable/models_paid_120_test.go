package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func TestModels120PaidValidationAndReservedAuthority(t *testing.T) {
	if e := validatePaidClassifierContext(goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{}}); e == nil {
		t.Fatal("empty questions accepted")
	}
	if e := validatePaidImageContext(images.ImagesContext{}); e == nil {
		t.Fatal("empty image input accepted")
	}
	if e := validatePaidImageContext(images.ImagesContext{Input: []images.ImageInput{{Type: "image", Data: "x"}}}); e == nil {
		t.Fatal("missing MIME accepted")
	}
	registry := NewRegistry()
	reg := wrapRegistration(classifierToolName)
	if e := registry.Register(reg); e == nil {
		t.Fatal("reserved adapter forgery")
	}
	if e := installPaidModelTools(registry); e != nil {
		t.Fatal(e)
	}
	if e := installPaidModelTools(registry); e != nil {
		t.Fatal("reopen registration", e)
	}
	for _, name := range []string{classifierToolName, imagesToolName} {
		tool, ok := registry.current(name)
		if !ok || tool.offer.ReplaySafe || toolCallerAllowed(tool.offer, "model") || !toolCallerAllowed(tool.offer, "tools") {
			t.Fatal(name, tool.offer)
		}
	}
}

func TestModels120PaidTypedCallsDurableScopedAuthUsageAndKeyReuse(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var classifierRuns, imageRuns, optionsRuns atomic.Int64
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "paid-test-classifier", Classify: func(model *goai.ClassifierModel, input goai.ClassifierContext, opts *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			classifierRuns.Add(1)
			if opts.APIKey != "host-key" || opts.Context == nil || model.BaseURL != "" || model.Headers != nil {
				t.Error("classifier boundary", model, opts)
			}
			return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{"q": {Type: "bool", Probability: 1}}, StopReason: "stop", Usage: &goai.Usage{Input: 2, Output: 1, TotalTokens: 3}}, nil
		}})

		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "paid-test-images", GenerateImages: func(model *images.ImagesModel, input images.ImagesContext, opts *images.ImagesOptions) (*images.AssistantImages, error) {
			imageRuns.Add(1)
			if opts.APIKey != "image-key" || opts.Context == nil || model.BaseURL != "" || model.Headers != nil || len(input.Input) != 1 {
				t.Error("image boundary", model, opts)
			}
			return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}, StopReason: "stop", Usage: &goai.Usage{Input: 4, Output: 2, TotalTokens: 6}}, nil
		}})
		defer images.UnregisterImagesApiProvider("paid-test-images")
		catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image", Api: "paid-test-images", Headers: map[string]string{"Authorization": "private"}, BaseURL: "private"}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier", Api: "paid-test-classifier", Headers: map[string]string{"Authorization": "private"}, BaseURL: "private"}}
		registry := NewRegistry()
		def := taskDefinition(t, "paid-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			input := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "bool", Instructions: "yes?", Criteria: goai.ClassifierBoolCriteria{True: "yes", False: "no"}}}}
			for i := 0; i < 2; i++ {
				result, e := access.Classify(ctx, "classify", goai.TypedModelRef{Provider: "scoped", ID: "classifier"}, input)
				if e != nil {
					return e
				}
				if result.Answers["q"].Probability != 1 {
					t.Fatal(result)
				}
			}
			result, e := access.GenerateImages(ctx, "images", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
			if e != nil {
				return e
			}
			if len(result.Output) != 1 || result.Output[0].Data != "aGVsbG8=" {
				t.Fatal(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true, ClassifierRequestOptions: func(ctx context.Context, ref goai.TypedModelRef) (*goai.ClassifierOptions, error) {
			optionsRuns.Add(1)
			if ref.Type != goai.ModelTypeClassifier {
				t.Error(ref)
			}
			return &goai.ClassifierOptions{APIKey: "host-key"}, nil
		}, ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) {
			optionsRuns.Add(1)
			return &images.ImagesOptions{APIKey: "image-key"}, nil
		}})
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		if classifierRuns.Load() != 1 || imageRuns.Load() != 1 || optionsRuns.Load() != 2 {
			t.Fatal("paid key duplicated dispatch", classifierRuns.Load(), imageRuns.Load(), optionsRuns.Load())
		}
		usage, e := h.Usage(bg)
		if e != nil {
			t.Fatal(e)
		}
		if usage.Tools[classifierToolName].TotalTokens != 3 || usage.Tools[imagesToolName].TotalTokens != 6 {
			t.Fatal("paid usage missing", usage)
		}
		if e = h.Close(bg); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		if _, e = next.Snapshot(bg); e != nil {
			t.Fatal(e)
		}
		if classifierRuns.Load() != 1 || imageRuns.Load() != 1 {
			t.Fatal("open repeated paid effects")
		}
	})
}
