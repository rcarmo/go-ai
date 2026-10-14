package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func paidImageCatalog120(api images.ImagesApi) *typedCatalog120 {
	return &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image", Api: goai.ImageApi(api)}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier"}}
}
func TestModels120PaidImageUnsafeRestartDoesNotRedispatch(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		const api images.ImagesApi = "paid-restart-test"
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var runs, optionsCalls atomic.Int64
		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(_ *images.ImagesModel, _ images.ImagesContext, options *images.ImagesOptions) (*images.AssistantImages, error) {
			runs.Add(1)
			close(entered)
			<-options.Context.Done()
			close(cancelled)
			<-release
			return nil, options.Context.Err()
		}})
		defer images.UnregisterImagesApiProvider(api)
		registry := NewRegistry()
		var attempts atomic.Int64
		def := taskDefinition(t, "paid-restart-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			_, e = access.GenerateImages(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
			if attempts.Add(1) == 1 {
				return e
			}
			if e == nil {
				return reject("interrupted paid call unexpectedly produced structured output")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api), ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) {
			optionsCalls.Add(1)
			return &images.ImagesOptions{APIKey: "host"}, nil
		}})
		cleanupTaskGates(t, release)
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, entered)
		closed := make(chan error, 1)
		go func() { closed <- h.Close(bg) }()
		awaitTaskSignal(t, cancelled)
		select {
		case e := <-closed:
			t.Fatal("paid close skipped actual provider return", e)
		default:
		}
		releaseTaskGate(release)
		if e := watchReceive(t, closed); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || runs.Load() != 1 || optionsCalls.Load() != 1 {
			t.Fatalf("outcome:%+v error:%+v runs:%d options:%d", record.State.Outcome, record.State.Outcome.Error, runs.Load(), optionsCalls.Load())
		}
		state, e := next.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, doc := range state.Documents {
			if doc.Kind == nestedCallDocument {
				var call nestedCallState
				if e := fromObject(doc.Value, &call, next.session.limits); e != nil {
					t.Fatal(e)
				}
				if call.Name == imagesToolName {
					found = true
					if call.Result == nil || call.Result.Code != "interrupted" || call.Result.HasStructuredOutput {
						t.Fatal("unsafe restart result", call)
					}
				}
			}
		}
		if !found {
			t.Fatal("paid task/result missing")
		}
	})
}
func TestModels120PaidImageProviderErrorRetainsBillingOnce(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		const api images.ImagesApi = "paid-error-test"
		var runs atomic.Int64
		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
			runs.Add(1)
			return &images.AssistantImages{StopReason: goai.StopReasonError, ErrorMessage: "provider rejected", Usage: &goai.Usage{Input: 5, Output: 1, TotalTokens: 6}}, nil
		}})
		defer images.UnregisterImagesApiProvider(api)
		registry := NewRegistry()
		def := taskDefinition(t, "paid-error-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			for i := 0; i < 2; i++ {
				result, e := access.GenerateImages(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
				if e != nil {
					return e
				}
				if result.StopReason != goai.StopReasonError || result.ErrorMessage != "provider rejected" || result.Usage.TotalTokens != 6 {
					t.Fatal(result)
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api)})
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		usage, e := h.Usage(bg)
		if e != nil || usage.Tools[imagesToolName].TotalTokens != 6 || runs.Load() != 1 {
			t.Fatal("billed error lost/duplicated", usage, e, runs.Load())
		}
		if e = h.Close(bg); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		usage, e = next.Usage(bg)
		if e != nil || usage.Tools[imagesToolName].TotalTokens != 6 || runs.Load() != 1 {
			t.Fatal("reopen changed billing", usage, e, runs.Load())
		}
	})
}
