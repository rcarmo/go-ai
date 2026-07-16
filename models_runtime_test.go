package goai_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestModelRuntimeRefreshRestoresCacheFetchesAndPersists(t *testing.T) {
	store := goai.NewInMemoryModelsStore()
	cached := runtimeModel("cached")
	if err := store.Write("dynamic", &goai.ModelsStoreEntry{Models: []*goai.Model{cached}, CheckedAt: 1}); err != nil {
		t.Fatal(err)
	}
	provider := &testDynamicProvider{id: "dynamic", refreshed: []*goai.Model{runtimeModel("fresh")}}
	runtime := goai.NewModelRuntime(store)
	runtime.SetProvider(provider)
	result := runtime.Refresh(t.Context(), true)
	if result.Aborted || len(result.Errors) != 0 {
		t.Fatalf("unexpected refresh result: %#v", result)
	}
	if got := runtime.GetModel("dynamic", "fresh"); got == nil {
		t.Fatalf("fresh model missing after refresh: %#v", runtime.GetModels("dynamic"))
	}
	stored, _ := store.Read("dynamic")
	if stored == nil || len(stored.Models) != 1 || stored.Models[0].ID != "fresh" || stored.CheckedAt == 0 {
		t.Fatalf("unexpected stored entry: %#v", stored)
	}
	if provider.calls != 1 {
		t.Fatalf("refresh calls=%d", provider.calls)
	}
}

func TestModelRuntimeRefreshRetainsCachedModelsOnFetchError(t *testing.T) {
	store := goai.NewInMemoryModelsStore()
	_ = store.Write("dynamic", &goai.ModelsStoreEntry{Models: []*goai.Model{runtimeModel("cached")}, CheckedAt: 1})
	provider := &testDynamicProvider{id: "dynamic", err: errors.New("boom")}
	runtime := goai.NewModelRuntime(store)
	runtime.SetProvider(provider)
	result := runtime.Refresh(t.Context(), true)
	if result.Errors["dynamic"] == nil {
		t.Fatalf("expected provider error: %#v", result)
	}
	if got := runtime.GetModel("dynamic", "cached"); got == nil {
		t.Fatalf("cached model should remain after error: %#v", runtime.GetModels("dynamic"))
	}
}

func TestModelRuntimeRefreshAllowNetworkFalseOnlyRestoresCache(t *testing.T) {
	store := goai.NewInMemoryModelsStore()
	_ = store.Write("dynamic", &goai.ModelsStoreEntry{Models: []*goai.Model{runtimeModel("cached")}, CheckedAt: 1})
	provider := &testDynamicProvider{id: "dynamic", refreshed: []*goai.Model{runtimeModel("fresh")}}
	runtime := goai.NewModelRuntime(store)
	runtime.SetProvider(provider)
	result := runtime.Refresh(t.Context(), false)
	if result.Aborted || len(result.Errors) != 0 || provider.calls != 0 {
		t.Fatalf("unexpected refresh result/calls: %#v calls=%d", result, provider.calls)
	}
	if runtime.GetModel("dynamic", "cached") == nil || runtime.GetModel("dynamic", "fresh") != nil {
		t.Fatalf("expected only cached models: %#v", runtime.GetModels("dynamic"))
	}
}

func TestModelRuntimeRefreshDeduplicatesConcurrentFetches(t *testing.T) {
	provider := &testDynamicProvider{id: "dynamic", refreshed: []*goai.Model{runtimeModel("fresh")}, block: make(chan struct{})}
	runtime := goai.NewModelRuntime(nil)
	runtime.SetProvider(provider)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); runtime.Refresh(t.Context(), true) }()
	go func() { defer wg.Done(); runtime.Refresh(t.Context(), true) }()
	deadline := time.After(time.Second)
	for {
		provider.mu.Lock()
		calls := provider.calls
		provider.mu.Unlock()
		if calls == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("refresh did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(provider.block)
	wg.Wait()
	if provider.calls != 1 {
		t.Fatalf("expected one fetch, got %d", provider.calls)
	}
}

func TestModelRuntimeRefreshCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	provider := &testDynamicProvider{id: "dynamic", refreshed: []*goai.Model{runtimeModel("fresh")}}
	runtime := goai.NewModelRuntime(nil)
	runtime.SetProvider(provider)
	result := runtime.Refresh(ctx, true)
	if !result.Aborted || provider.calls != 0 {
		t.Fatalf("expected aborted without fetch: %#v calls=%d", result, provider.calls)
	}
}

func TestModelRuntimeRefreshWithOptionsPassesForce(t *testing.T) {
	provider := &testDynamicProvider{id: "dynamic", refreshed: []*goai.Model{runtimeModel("fresh")}}
	runtime := goai.NewModelRuntime(nil)
	runtime.SetProvider(provider)
	result := runtime.RefreshWithOptions(t.Context(), goai.ModelRuntimeRefreshOptions{AllowNetwork: true, Force: true})
	if result.Aborted || len(result.Errors) != 0 {
		t.Fatalf("unexpected refresh result: %#v", result)
	}
	if !provider.lastForce {
		t.Fatal("provider did not receive Force=true")
	}
}

type testDynamicProvider struct {
	id        goai.Provider
	refreshed []*goai.Model
	err       error
	block     chan struct{}
	mu        sync.Mutex
	calls     int
	lastForce bool
}

func (p *testDynamicProvider) ID() string                  { return string(p.id) }
func (p *testDynamicProvider) StaticModels() []*goai.Model { return nil }
func (p *testDynamicProvider) RefreshModels(ctx goai.ModelRefreshContext) ([]*goai.Model, error) {
	p.mu.Lock()
	p.calls++
	p.lastForce = ctx.Force
	p.mu.Unlock()
	if p.block != nil {
		select {
		case <-ctx.Signal.Done():
			return nil, ctx.Signal.Err()
		case <-p.block:
		}
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.refreshed, nil
}

func runtimeModel(id string) *goai.Model {
	return &goai.Model{ID: id, Name: id, Provider: "dynamic", Api: goai.ApiOpenAICompletions, Input: []string{"text"}, Cost: goai.ModelCost{}, ContextWindow: 1000, MaxTokens: 100}
}
