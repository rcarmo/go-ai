package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestModels110CompleteUsesHostRequestBoundaryAndLifetime(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		api := goai.Api("model-access-" + t.Name())
		requests := 0
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, _ *goai.Context, options *goai.StreamOptions) <-chan goai.Event {
			requests++
			if options == nil || options.APIKey != "host-secret" {
				t.Error("host credential boundary lost", options)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("nested")
			close(ch)
			return ch
		}})
		defer goai.UnregisterApi(api)
		catalog := goai.NewModelRuntime(nil)
		catalog.SetProvider(goai.StaticModelProvider{Provider: "scoped", Models: []*goai.Model{{ID: "model", Name: "model", Provider: "scoped", Api: api, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100}}})
		var access *ModelAccess
		definition := taskDefinition(t, "nested-request", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			var e error
			access, e = r.Models()
			if e != nil {
				return e
			}
			message, e := access.Complete(ctx, ModelRef{Provider: "scoped", ID: "model"}, &goai.Context{Messages: []goai.Message{goai.UserMessage("nested")}})
			if e != nil {
				return e
			}
			if message.Content[0].Text != "nested" {
				t.Fatal(message)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("ok"), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Catalog: catalog, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
			return &goai.StreamOptions{APIKey: "host-secret"}, nil
		}}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		waitPublicTask(t, h, id)
		if requests != 1 {
			t.Fatal(requests)
		}
		if _, e := access.Complete(bg, ModelRef{}, nil); !errors.Is(e, ErrSealed) {
			t.Fatal("ended model request admitted", e)
		}
	})
}
