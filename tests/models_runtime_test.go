package goai_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

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
	synctest.Test(t, func(t *testing.T) {
		provider := &testDynamicProvider{id: "dynamic", refreshed: []*goai.Model{runtimeModel("fresh")}, block: make(chan struct{})}
		defer close(provider.block)
		runtime := goai.NewModelRuntime(nil)
		runtime.SetProvider(provider)
		first := startModelRefreshV160(t.Context(), runtime, false)
		synctest.Wait()
		second := startModelRefreshV160(t.Context(), runtime, false)
		synctest.Wait()
		if provider.calls != 1 {
			t.Fatalf("expected one shared fetch, got %d", provider.calls)
		}
		provider.block <- struct{}{}
		synctest.Wait()
		assertRefreshResultV160(t, first, "first refresh", false)
		assertRefreshResultV160(t, second, "joined refresh", false)
	})
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

// Source: pi v1.6.0 packages/ai/test/providers.test.ts:710–754.
// Use Force for the newer Go call: ordinary Go refreshes retain singleflight.
func TestSourceV160ModelRuntimeRefreshSupersedesOlderNetworkWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked := make(chan struct{})
		defer close(blocked)
		secondBlocked := make(chan struct{})
		defer close(secondBlocked)
		var calls int
		var firstSignal context.Context
		provider := refreshFuncV160{fetch: func(opts goai.ModelRefreshContext) ([]*goai.Model, error) {
			calls++
			current := calls
			if current == 1 {
				firstSignal = opts.Signal
				<-blocked // Deliberately ignores cancellation, like the pinned fetch.
				return []*goai.Model{runtimeModel("listed-1")}, nil
			}
			<-secondBlocked
			return []*goai.Model{runtimeModel("listed-2")}, nil
		}}
		store := goai.NewInMemoryModelsStore()
		runtime := goai.NewModelRuntime(store)
		runtime.SetProvider(provider)
		if got := runtime.GetModels("dynamic"); len(got) != 0 {
			t.Fatalf("initial models = %+v, want empty", got)
		}
		first := startModelRefreshV160(t.Context(), runtime, false)
		synctest.Wait()
		if calls != 1 || firstSignal == nil {
			t.Fatalf("first refresh did not start: calls=%d", calls)
		}
		second := startModelRefreshV160(t.Context(), runtime, true)
		synctest.Wait()
		if calls != 2 {
			t.Fatalf("newer forced refresh did not bypass the older network work: fetches=%d, want 2", calls)
		}
		assertRefreshResultV160(t, first, "superseded refresh", false)
		if firstSignal.Err() != context.Canceled {
			t.Fatalf("superseded producer signal = %v, want canceled", firstSignal.Err())
		}
		secondBlocked <- struct{}{}
		synctest.Wait()
		assertRefreshResultV160(t, second, "newer forced refresh", false)
		assertModelPublicationV160(t, runtime, store, "listed-2")

		// Drop the first fetch's last gate and wait for every runnable worker,
		// including a late publication, without using a sleep.
		blocked <- struct{}{}
		synctest.Wait()
		assertModelPublicationV160(t, runtime, store, "listed-2")
	})
}

// Supplemental: a canceled caller must not publish a result from an
// uncooperative provider, and a canceled joiner cannot stop a shared refresh.
func TestModelRuntimeRefreshInFlightCancellationV160(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cancelMain bool
	}{
		{"owner", true},
		{"joiner", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				blocked := make(chan struct{})
				defer close(blocked)
				var calls int
				provider := refreshFuncV160{fetch: func(goai.ModelRefreshContext) ([]*goai.Model, error) {
					calls++
					<-blocked
					return []*goai.Model{runtimeModel("listed-1")}, nil
				}}
				store := goai.NewInMemoryModelsStore()
				runtime := goai.NewModelRuntime(store)
				runtime.SetProvider(provider)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				mainCtx := t.Context()
				if tc.cancelMain {
					mainCtx = ctx
				}
				main := startModelRefreshV160(mainCtx, runtime, false)
				synctest.Wait()
				canceled := main
				if !tc.cancelMain {
					canceled = startModelRefreshV160(ctx, runtime, false)
					synctest.Wait()
				}
				cancel()
				synctest.Wait()
				assertRefreshResultV160(t, canceled, "canceled caller", true)
				if calls != 1 {
					t.Fatalf("network fetches = %d, want 1", calls)
				}
				blocked <- struct{}{}
				synctest.Wait()
				if !tc.cancelMain {
					assertRefreshResultV160(t, main, "shared refresh", false)
					assertModelPublicationV160(t, runtime, store, "listed-1")
					return
				}
				if got := runtime.GetModels("dynamic"); len(got) != 0 {
					t.Errorf("canceled refresh published models: %+v", got)
				}
				if got, err := store.Read("dynamic"); err != nil || got != nil {
					t.Errorf("canceled refresh stored %+v, err=%v; want no stored entry", got, err)
				}
			})
		})
	}
}

type refreshFuncV160 struct {
	fetch func(goai.ModelRefreshContext) ([]*goai.Model, error)
}

func (refreshFuncV160) ID() string                  { return "dynamic" }
func (refreshFuncV160) StaticModels() []*goai.Model { return nil }
func (p refreshFuncV160) RefreshModels(opts goai.ModelRefreshContext) ([]*goai.Model, error) {
	return p.fetch(opts)
}

func startModelRefreshV160(ctx context.Context, runtime *goai.ModelRuntime, force bool) <-chan goai.ModelRuntimeRefreshResult {
	result := make(chan goai.ModelRuntimeRefreshResult, 1)
	go func() {
		result <- runtime.RefreshWithOptions(ctx, goai.ModelRuntimeRefreshOptions{AllowNetwork: true, Force: force})
	}()
	return result
}

func assertRefreshResultV160(t *testing.T, result <-chan goai.ModelRuntimeRefreshResult, name string, wantAborted bool) {
	t.Helper()
	select {
	case got := <-result:
		if got.Aborted != wantAborted || len(got.Errors) != 0 {
			t.Fatalf("%s result = %+v, want aborted=%t and no errors", name, got, wantAborted)
		}
	default:
		t.Fatalf("%s did not return while the older network work was blocked", name)
	}
}

func assertModelPublicationV160(t *testing.T, runtime *goai.ModelRuntime, store goai.ModelsStore, wantID string) {
	t.Helper()
	if got := runtime.GetModels("dynamic"); len(got) != 1 || got[0].ID != wantID {
		t.Errorf("runtime models = %+v, want only %q", got, wantID)
	}
	if got, err := store.Read("dynamic"); err != nil || got == nil || len(got.Models) != 1 || got.Models[0].ID != wantID {
		t.Errorf("stored entry = %+v, err=%v; want only %q", got, err, wantID)
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
