package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

type countedAuthCatalog120 struct {
	*authCatalog120
	calls atomic.Int64
}

func (c *countedAuthCatalog120) GetAvailableModels(ctx context.Context, kind goai.ModelType, provider goai.Provider) ([]any, error) {
	c.calls.Add(1)
	return c.authCatalog120.GetAvailableModels(ctx, kind, provider)
}
func TestCodemode120AvailableCatalogueReopenReusesSanitizedDelivery(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		catalog := &countedAuthCatalog120{authCatalog120: &authCatalog120{typedCatalog120: &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image", Type: "image", Headers: map[string]string{"Authorization": "private"}, BaseURL: "https://private.invalid"}}}}
		catalog.ModelRuntime.SetProvider(goai.StaticModelProvider{Provider: "scoped", Models: []*goai.Model{{Provider: "scoped", ID: "chat", Headers: map[string]string{"Authorization": "private"}, APIKey: "private", BaseURL: "https://private.invalid"}}})
		sandbox := &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}
		registry := NewRegistry()
		def := taskDefinition(t, "available-reopen-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const images=await models.getAvailableOfType("image","scoped");if(images.length!==1||images[0].id!=="image"||images[0].headers||images[0].baseUrl)throw Error("image leak");const chats=await models.getModelsOfType("chat","scoped");if(chats.length!==1||chats[0].headers||chats[0].apiKey||chats[0].baseUrl)throw Error("chat leak");return images[0].id;`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != "image" {
				t.Error(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		if catalog.calls.Load() != 1 {
			t.Fatal("availability calls", catalog.calls.Load())
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		catalog.fail = errors.New("auth backend offline")
		catalog.image = nil
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || catalog.calls.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("%+v %+v calls=%d starts=%d", record.State.Outcome, record.State.Outcome.Error, catalog.calls.Load(), sandbox.starts.Load())
		}
	})
}
