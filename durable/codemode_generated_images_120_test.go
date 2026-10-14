package durable

import (
	"bytes"
	"context"
	"encoding/base64"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"image"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120GeneratedImageNoticeUsesConfirmedReplay(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(encoded.Bytes())
	api := images.ImagesApi("unshown-" + t.Name())
	var effects atomic.Int64
	images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
		effects.Add(1)
		return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", MimeType: "image/png", Data: data}}, StopReason: goai.StopReasonStop}, nil
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
		writer := &outputWriter120{}
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
			shownCode := `const result=await models.generateImages({provider:"scoped",id:"image"},{input:[{type:"text",text:"a dot"}]});image(result.output[0]);`
			for i := 0; i < 2; i++ {
				result, err := r.RunCodemode(ctx, "shown", shownCode, engine)
				if err != nil {
					return err
				}
				if result.Error != "" || strings.Contains(result.Output, "script did not show") || len(result.Images) != 1 || !strings.Contains(result.Output, "/owned/image-1.png") {
					t.Errorf("shown image %+v", result)
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api), Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 2 {
			t.Fatal(record.State.Outcome, effects.Load())
		}
		writer.mu.Lock()
		defer writer.mu.Unlock()
		if len(writer.writes) != 1 || writer.count != 1 {
			t.Fatal("shown image writer replay", writer.writes)
		}
		decoded, err := png.Decode(strings.NewReader(writer.writes["image-1.png"]))
		if err != nil || decoded.Bounds().Dx() != 1 {
			t.Fatal("saved image", err)
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
