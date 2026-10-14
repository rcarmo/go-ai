package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func TestModels120PaidImageSettlementFailureNeverRepeatsPaidEffect(t *testing.T) {
	for _, mode := range []string{"rejected", "uncertain-before", "uncertain-after"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				const api images.ImagesApi = "paid-settlement-test"
				var runs atomic.Int64
				images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
					runs.Add(1)
					return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}, StopReason: goai.StopReasonStop, Usage: &goai.Usage{Input: 5, Output: 1, TotalTokens: 6}}, nil
				}})
				defer images.UnregisterImagesApiProvider(api)
				registry := NewRegistry()
				var first atomic.Bool
				first.Store(true)
				def := taskDefinition(t, "paid-settlement-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					access, e := r.Models()
					if e != nil {
						return e
					}
					_, e = access.GenerateImages(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
					if first.Load() {
						return e
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api)})
				root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				core := h.session.core
				original := core.appendFrame
				cause := reject("paid settlement rejected")
				var failures atomic.Int64
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var record commitRecord
						if e := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); e != nil {
							return e
						}
						for _, write := range record.Writes {
							if write.Task != nil && write.Task.Kind == nestedToolKind && taskHasDecidedOutcome(*write.Task) {
								failures.Add(1)
								if mode == "rejected" {
									return cause
								}
								if mode == "uncertain-after" && original != nil {
									if e := original(kind, ordinal, high, payload); e != nil {
										return e
									}
								}
								return errors.New("uncertain paid settlement")
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				if e := h.Resume(bg); e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				end, e := h.session.Closed(ctx)
				if e != nil || end.Reason != "failed" {
					t.Fatal(end, e)
				}
				if mode == "rejected" && !errors.Is(end.Error, cause) || mode != "rejected" && !errors.Is(end.Error, ErrPoisoned) {
					t.Fatal(end)
				}
				if e = h.Close(ctx); e != nil {
					t.Fatal(e)
				}
				reopened := reopenStoreAfterHarnessClose(t, b.store)
				state, e := reopened.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				_, journal := b.store.(*JournalStorage)
				confirmed := mode == "uncertain-after" && journal
				for _, doc := range state.Documents {
					if doc.Kind == nestedCallDocument {
						var call nestedCallState
						if e = fromObject(doc.Value, &call, h.session.limits); e != nil || (call.Result != nil) != confirmed {
							t.Fatal("partial paid result", call, e)
						}
					}
				}
				first.Store(false)
				next := openHarness(t, reopened, h.options)
				record := waitPublicTask(t, next, id)
				if record.State.Outcome.Status != "completed" || runs.Load() != 1 || failures.Load() != 1 {
					t.Fatalf("paid replay:%+v %+v runs:%d writes:%d", record.State.Outcome, record.State.Outcome.Error, runs.Load(), failures.Load())
				}
				usage, e := next.Usage(bg)
				if e != nil {
					t.Fatal(e)
				}
				want := 0
				if confirmed {
					want = 6
				}
				if usage.Tools[imagesToolName].TotalTokens != want {
					t.Fatal("unconfirmed paid bill fabricated/confirmed bill lost", usage, want)
				}
			})
		})
	}
}
