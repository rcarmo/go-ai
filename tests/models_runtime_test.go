package goai_test

import (
	"context"
	"errors"
	"slices"
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

func TestModelRuntimeCacheOnlyRefreshJoinsWithoutSuperseding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := &testDynamicProvider{id: "dynamic", refreshed: []*goai.Model{runtimeModel("fresh")}, block: make(chan struct{})}
		defer close(provider.block)
		runtime := goai.NewModelRuntime(nil)
		runtime.SetProvider(provider)
		first := startModelRefreshV160(t.Context(), runtime, true)
		synctest.Wait()
		cacheOnly := startModelRefreshV160(t.Context(), runtime, false)
		synctest.Wait()
		select {
		case got := <-first:
			t.Fatalf("cache-only refresh did not preserve the in-flight fetch: %v", got)
		case got := <-cacheOnly:
			t.Fatalf("cache-only refresh did not preserve the in-flight fetch: waiter returned early: %v", got)
		default:
		}
		if provider.calls != 1 {
			t.Fatalf("expected one shared fetch, got %d", provider.calls)
		}
		provider.block <- struct{}{}
		synctest.Wait()
		assertRefreshResultV160(t, first, "network refresh", false)
		assertRefreshResultV160(t, cacheOnly, "cache-only refresh", false)
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
func TestSourceV160ModelRuntimeRefreshSupersedesOlderNetworkWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked := make(chan struct{})
		defer close(blocked)
		secondBlocked := make(chan struct{})
		defer close(secondBlocked)
		var calls int
		var firstSignal context.Context
		provider := refreshFuncV160{fetch: func(opts goai.ModelRefreshContext) ([]*goai.Model, error) {
			if opts.Force {
				t.Error("default refresh forced the provider to bypass its cache")
			}
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
		firstCtx, cancelFirst := context.WithCancel(t.Context())
		defer cancelFirst()
		first := startModelRefreshV160(firstCtx, runtime, true)
		synctest.Wait()
		if calls != 1 || firstSignal == nil {
			t.Fatalf("first refresh did not start: calls=%d", calls)
		}
		second := startModelRefreshV160(t.Context(), runtime, true)
		synctest.Wait()
		if calls != 2 {
			t.Fatalf("newer default refresh did not bypass the older network work: fetches=%d, want 2", calls)
		}
		assertRefreshResultV160(t, first, "superseded refresh", false)
		if firstSignal.Err() != context.Canceled {
			t.Fatalf("superseded producer signal = %v, want canceled", firstSignal.Err())
		}
		cancelFirst() // The older caller no longer owns any part of the newer call.
		secondBlocked <- struct{}{}
		synctest.Wait()
		assertRefreshResultV160(t, second, "newer default refresh", false)
		assertModelPublicationV160(t, runtime, store, "listed-2")

		// Drop the first fetch's last gate and wait for every runnable worker,
		// including a late publication, without using a sleep.
		blocked <- struct{}{}
		synctest.Wait()
		assertModelPublicationV160(t, runtime, store, "listed-2")
	})
}

// Supplemental: a canceled caller must not publish a result from an
// uncooperative provider, and a canceled cache-only waiter cannot stop a fetch.
func TestModelRuntimeRefreshInFlightCancellationV160(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cancelMain bool
	}{
		{"owner", true},
		{"cache_only_waiter", false},
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
				main := startModelRefreshV160(mainCtx, runtime, true)
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

// Supplemental: a store write admitted before supersession cannot be undone,
// but it must finish before the successor can publish or persist its models.
func TestModelRuntimeRefreshOrdersInFlightStoreWriteV160(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		unblock := make(chan struct{})
		defer close(unblock)
		writeStarted := make(chan struct{})
		var commits []string
		var commitsMu sync.Mutex
		base := goai.NewInMemoryModelsStore()
		store := refreshStoreV160{
			ModelsStore: base,
			write: func(provider goai.Provider, entry *goai.ModelsStoreEntry) error {
				id := entry.Models[0].ID
				if id == "listed-1" {
					close(writeStarted)
					<-unblock
				}
				if err := base.Write(provider, entry); err != nil {
					return err
				}
				commitsMu.Lock()
				commits = append(commits, id)
				commitsMu.Unlock()
				return nil
			},
		}
		runtime := goai.NewModelRuntime(store)
		var fetches int
		runtime.SetProvider(refreshFuncV160{fetch: func(goai.ModelRefreshContext) ([]*goai.Model, error) {
			fetches++
			if fetches == 1 {
				return []*goai.Model{runtimeModel("listed-1")}, nil
			}
			return []*goai.Model{runtimeModel("listed-2")}, nil
		}})
		first := startModelRefreshV160(t.Context(), runtime, true)
		<-writeStarted
		second := startModelRefreshV160(t.Context(), runtime, true)
		// The successor may already be waiting on the store mutex. Read the
		// old caller's result directly; synctest.Wait does not idle on mutexes.
		if got := <-first; got.Aborted || len(got.Errors) != 0 {
			t.Fatalf("superseded refresh with a blocked store write = %+v", got)
		}
		if got := runtime.GetModels("dynamic"); len(got) != 0 {
			t.Fatalf("models published before the cache write finished: %+v", got)
		}
		unblock <- struct{}{}
		synctest.Wait()
		assertRefreshResultV160(t, second, "newer refresh after an ordered write", false)
		assertModelPublicationV160(t, runtime, store, "listed-2")
		if !slices.Equal(commits, []string{"listed-1", "listed-2"}) {
			t.Fatalf("write order = %v, want [listed-1 listed-2]", commits)
		}
	})
}

type refreshStoreV160 struct {
	goai.ModelsStore
	write func(goai.Provider, *goai.ModelsStoreEntry) error
}

func (s refreshStoreV160) Write(provider goai.Provider, entry *goai.ModelsStoreEntry) error {
	return s.write(provider, entry)
}

type refreshFuncV160 struct {
	fetch func(goai.ModelRefreshContext) ([]*goai.Model, error)
}

func (refreshFuncV160) ID() string                  { return "dynamic" }
func (refreshFuncV160) StaticModels() []*goai.Model { return nil }
func (p refreshFuncV160) RefreshModels(opts goai.ModelRefreshContext) ([]*goai.Model, error) {
	return p.fetch(opts)
}

func startModelRefreshV160(ctx context.Context, runtime *goai.ModelRuntime, allowNetwork bool) <-chan goai.ModelRuntimeRefreshResult {
	result := make(chan goai.ModelRuntimeRefreshResult, 1)
	go func() { result <- runtime.Refresh(ctx, allowNetwork) }()
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

func TestModelRuntimeTypedCacheDetachesAndReadsLegacyEntries(t *testing.T) {
	const id = "typed-cache"
	store := goai.NewInMemoryModelsStore()
	chat := &goai.Model{Provider: id, ID: "chat"}
	image := &goai.ImageModel{Provider: id, ID: "image", Input: []string{"image"}, Headers: map[string]string{"X-Test": "original"},
		InputLimits: &goai.ModelInputLimits{Images: &goai.ModelImageInputLimits{Resize: &goai.ModelImageResizeOptions{MaxWidth: 42}}},
		Cost:        goai.ModelCost{ImageOutput: new(2.5), Tiers: []goai.ModelCostTier{{Output: 3}}}}
	decision := &goai.ClassifierModel{Provider: id, ID: "decision", Input: []string{"text"}, Headers: map[string]string{"X-Test": "original"},
		Cost: goai.ModelCost{ImageOutput: new(1.5), Tiers: []goai.ModelCostTier{{Output: 2}}}}
	if err := store.Write(id, &goai.ModelsStoreEntry{Models: []*goai.Model{chat},
		ImageModels: []*goai.ImageModel{image, {Provider: "foreign", ID: "other"}}, ClassifierModels: []*goai.ClassifierModel{decision, {Provider: "foreign", ID: "other"}}}); err != nil {
		t.Fatal(err)
	}
	image.Headers["X-Test"] = "mutated"
	image.InputLimits.Images.Resize.MaxWidth = 0
	*image.Cost.ImageOutput = 0
	image.Cost.Tiers[0].Output = 0
	decision.Headers["X-Test"] = "mutated"
	*decision.Cost.ImageOutput = 0
	decision.Cost.Tiers[0].Output = 0

	runtime := goai.NewModelRuntime(store)
	runtime.SetProvider(goai.StaticModelProvider{Provider: id})
	if got := runtime.Refresh(t.Context(), false); got.Aborted || len(got.Errors) != 0 {
		t.Fatalf("cache-only refresh: %+v", got)
	}
	got := runtime.GetImageModel(id, "image")
	if got == nil || got.Headers["X-Test"] != "original" || got.InputLimits.Images.Resize.MaxWidth != 42 || *got.Cost.ImageOutput != 2.5 || got.Cost.Tiers[0].Output != 3 {
		t.Fatalf("image store was not detached: %+v", got)
	}
	got.Headers["X-Test"] = "reader"
	if runtime.GetImageModel(id, "image").Headers["X-Test"] != "original" {
		t.Fatal("runtime image reads share mutable headers")
	}
	if got := runtime.GetClassifierModel(id, "decision"); got == nil || got.Headers["X-Test"] != "original" || *got.Cost.ImageOutput != 1.5 || got.Cost.Tiers[0].Output != 2 {
		t.Fatalf("classifier store was not detached: %+v", got)
	}
	if runtime.GetImageModel(id, "other") != nil || runtime.GetClassifierModel(id, "other") != nil || len(runtime.GetModels(id)) != 1 {
		t.Fatal("typed catalog leaked foreign rows or changed the chat list")
	}
	if err := store.Write(id, &goai.ModelsStoreEntry{Models: []*goai.Model{chat}}); err != nil {
		t.Fatal(err)
	}
	if got := runtime.Refresh(t.Context(), false); got.Aborted || len(got.Errors) != 0 {
		t.Fatalf("legacy cache-only refresh: %+v", got)
	}
	if runtime.GetModel(id, "chat") == nil || runtime.GetImageModel(id, "image") != nil || runtime.GetClassifierModel(id, "decision") != nil {
		t.Fatal("legacy cache did not restore chat only")
	}
}
