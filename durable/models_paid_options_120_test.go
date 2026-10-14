package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func TestModels120PaidImageRequestOptionsBoundary(t *testing.T) {
	for _, mode := range []string{"rewrite", "scope", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				const api images.ImagesApi = "paid-options-test"
				var runs atomic.Int64
				images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
					runs.Add(1)
					return &images.AssistantImages{StopReason: goai.StopReasonStop}, nil
				}})
				defer images.UnregisterImagesApiProvider(api)
				registry := NewRegistry()
				def := taskDefinition(t, "paid-options-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					access, e := r.Models()
					if e != nil {
						return e
					}
					ref := goai.TypedModelRef{Provider: "scoped", ID: "image"}
					input := images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}
					if mode == "scope" {
						ref.Provider = "outside"
					}
					if mode == "invalid" {
						input.Input = nil
					}
					_, e = access.GenerateImages(ctx, "key", ref, input)
					if e == nil {
						return reject("paid boundary accepted invalid request")
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api), ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) {
					return &images.ImagesOptions{APIKey: "secret", OnPayload: func(payload map[string]any, _ *images.ImagesModel) (map[string]any, error) {
						payload["forged"] = true
						return payload, nil
					}}, nil
				}})
				root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
				record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
				if record.State.Outcome.Status != "completed" || runs.Load() != 0 {
					t.Fatalf("%+v %+v runs:%d", record.State.Outcome, record.State.Outcome.Error, runs.Load())
				}
				state, e := h.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				for _, task := range state.Tasks {
					data, e := encodeBounded(task, h.session.limits, h.session.limits.MaxRecordBytes)
					if e != nil {
						t.Fatal(e)
					}
					if strings.Contains(string(data), "secret") {
						t.Fatal("request key persisted in task")
					}
				}
				for _, doc := range state.Documents {
					data, e := encodeBounded(doc.Value, h.session.limits, h.session.limits.MaxDocumentBytes)
					if e != nil {
						t.Fatal(e)
					}
					if strings.Contains(string(data), "secret") {
						t.Fatal("request key persisted in document")
					}
				}
			})
		})
	}
}
