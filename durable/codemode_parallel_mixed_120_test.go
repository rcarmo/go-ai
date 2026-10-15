package durable

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/rcarmo/go-ai/images"
	"image"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestCodemode120ParallelMixedPaidCapacity(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		sandbox, err := NewQuickJSSandbox(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer sandbox.Close(bg)
		sandbox.ParallelCalls = true
		entered := make(chan struct{}, 6)
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var active, maximum, effects atomic.Int64
		api := goai.ClassifierApi("parallel-mixed-capacity-120")
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(_ *goai.ClassifierModel, _ goai.ClassifierContext, opts *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			effects.Add(1)
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			entered <- struct{}{}
			select {
			case <-release:
				return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{"yes": {Type: "bool", Probability: 1}}, StopReason: goai.StopReasonStop}, nil
			case <-opts.Context.Done():
				return nil, opts.Context.Err()
			}
		}})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
			t.Fatal(err)
		}
		imageAPI := images.ImagesApi("parallel-mixed-images-120")
		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: imageAPI, GenerateImages: func(_ *images.ImagesModel, _ images.ImagesContext, opts *images.ImagesOptions) (*images.AssistantImages, error) {
			effects.Add(1)
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			entered <- struct{}{}
			select {
			case <-release:
				return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(encoded.Bytes())}}, StopReason: goai.StopReasonStop}, nil
			case <-opts.Context.Done():
				return nil, opts.Context.Err()
			}
		}})
		defer images.UnregisterImagesApiProvider(imageAPI)
		registry := NewRegistry()
		def := taskDefinition(t, "parallel-capacity-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const m={provider:"test",id:"judge"};await Promise.all(Array.from({length:6},(_,i)=>i%2?models.generateImages({provider:"test",id:"image"},{input:[{type:"text",text:"dot"}]}):models.classify(m,{state:{},questions:{yes:{type:"bool",instructions:"yes?",criteria:{true:"yes",false:"no"}}}})));await host.ping("done");return "done";`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" {
				return reject(result.Error)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: &typedCatalog120{image: &goai.ImageModel{Provider: "test", ID: "image", Api: goai.ImageApi(imageAPI), Type: "image"}, classifier: &goai.ClassifierModel{Provider: "test", ID: "judge", Api: api}, ModelRuntime: goai.NewModelRuntime(nil)}, PaidModels: true, CodemodeGlobals: []CodemodeGlobal{{Name: "host.ping", Version: 1, Execute: func(context.Context, any) (any, error) { return "pong", nil }}}})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4; i++ {
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("four paid calls did not start")
			}
		}
		select {
		case <-entered:
			t.Fatal("paid capacity exceeded")
		case <-time.After(30 * time.Millisecond):
		}
		unblock()
		// Six paid nested calls incur race-instrumented journal/VM work.
		completion, stop := context.WithTimeout(bg, 15*time.Second)
		defer stop()
		record, err := h.WaitForTask(completion, id)
		if err != nil {
			t.Fatal(err)
		}
		if record.State.Outcome.Status != "completed" || maximum.Load() != 4 || effects.Load() != 6 {
			t.Fatal(record.State.Outcome, maximum.Load(), effects.Load())
		}
	})
}
