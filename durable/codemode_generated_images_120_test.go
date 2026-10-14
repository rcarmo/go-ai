package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120GeneratedImageNoticeUsesConfirmedReplay(t *testing.T) {
	api := images.ImagesApi("unshown-" + t.Name())
	var effects atomic.Int64
	images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
		effects.Add(1)
		return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", MimeType: "image/png", Data: "aGVsbG8="}}, StopReason: goai.StopReasonStop}, nil
	}})
	defer images.UnregisterImagesApiProvider(api)
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		effects.Store(0)
		registry := NewRegistry()
		def := taskDefinition(t, "image-notice", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := `await models.generateImages({provider:"scoped",id:"image"},{input:[{type:"text",text:"a dot"}]});text("generated");`
			for i := 0; i < 2; i++ {
				result, err := r.RunCodemode(ctx, "script", code, engine)
				if err != nil {
					return err
				}
				if result.Error != "" || !strings.Contains(result.Output, "Note: models.generateImages() returned 1 image that the script did not show.") {
					t.Errorf("notice %+v", result)
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api)})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 {
			t.Fatal(record.State.Outcome, effects.Load())
		}
	})
}
func TestCodemode120GeneratedNoticePluralAndShownSuppression(t *testing.T) {
	deliveries := []CodemodeDelivery{{Call: CodemodeCall{Global: true, Name: "models.generateImages"}, HasValue: true, Value: JSON{"output": []any{JSON{"type": "image"}, JSON{"type": "text"}, JSON{"type": "image"}}}}}
	if got := codemodeUnshownImageNotice(deliveries, 0); !strings.Contains(got, "returned 2 images") {
		t.Fatal(got)
	}
	if got := codemodeUnshownImageNotice(deliveries, 1); got != "" {
		t.Fatal(got)
	}
	deliveries[0].HasValue = false
	if got := codemodeUnshownImageNotice(deliveries, 0); got != "" {
		t.Fatal(got)
	}
}
