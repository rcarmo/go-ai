package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSImageGlobalScopedAndSettledReuse(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		var effects atomic.Int64
		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "codemode-image-120", GenerateImages: func(model *images.ImagesModel, input images.ImagesContext, opts *images.ImagesOptions) (*images.AssistantImages, error) {
			effects.Add(1)
			if opts.Context == nil || opts.APIKey != "host-key" || model.Headers != nil || model.BaseURL != "" || len(input.Input) != 1 {
				t.Error("image provider boundary", model, opts)
			}
			return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}, StopReason: "stop", Usage: &goai.Usage{Input: 4, Output: 2, TotalTokens: 6}}, nil
		}})
		registry := NewRegistry()
		def := taskDefinition(t, "image-global-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := `if(ALL_TOOLS.length!==0)throw Error("hidden");const result=await models.generateImages({provider:"test",id:"image"},{input:[{type:"text",text:"draw"}]});return result.output[0].data;`
			result, err := r.RunCodemode(ctx, "run", code, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != "aGVsbG8=" {
				t.Errorf("image global %+v", result)
			}
			if _, err = r.RunCodemode(ctx, "run", code, sandbox); err != nil {
				return err
			}
			if effects.Load() != 1 {
				t.Error("image effect repeated")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "test", ID: "image", Api: "codemode-image-120", Headers: map[string]string{"Authorization": "private"}, BaseURL: "https://private.invalid"}}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true, ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) {
			return &images.ImagesOptions{APIKey: "host-key"}, nil
		}})
		root(t, h, ModelRef{Provider: "test", ID: "chat"})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		usage, err := h.Usage(bg)
		if err != nil || usage.Tools[imagesToolName].TotalTokens != 6 {
			t.Fatal(usage, err)
		}
	})
}
func TestCodemode120QuickJSPaidGlobalsAbsentWithoutOptIn(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		def := taskDefinition(t, "paid-optout-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `return typeof models;`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != "undefined" {
				t.Error("paid globals exposed", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
