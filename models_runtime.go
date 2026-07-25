package goai

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ModelsStoreEntry is a provider-scoped cached dynamic model catalog.
type ModelsStoreEntry struct {
	Models       []*Model
	CheckedAt    int64
	LastModified int64
	ETag         string
}

// ModelsStore persists dynamic catalogs keyed by provider ID.
type ModelsStore interface {
	Read(provider Provider) (*ModelsStoreEntry, error)
	Write(provider Provider, entry *ModelsStoreEntry) error
	Delete(provider Provider) error
}

// InMemoryModelsStore is a deterministic in-memory ModelsStore.
type InMemoryModelsStore struct {
	mu      sync.RWMutex
	entries map[Provider]*ModelsStoreEntry
}

func NewInMemoryModelsStore() *InMemoryModelsStore {
	return &InMemoryModelsStore{entries: map[Provider]*ModelsStoreEntry{}}
}

func (s *InMemoryModelsStore) Read(provider Provider) (*ModelsStoreEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry := s.entries[provider]
	return cloneModelsStoreEntry(entry), nil
}

func (s *InMemoryModelsStore) Write(provider Provider, entry *ModelsStoreEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[provider] = cloneModelsStoreEntry(entry)
	return nil
}

func (s *InMemoryModelsStore) Delete(provider Provider) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, provider)
	return nil
}

type ModelRefreshContext struct {
	Provider     Provider
	Store        ModelsStore
	AllowNetwork bool
	// Force asks dynamic providers to bypass their own freshness checks and fetch immediately.
	Force  bool
	Signal context.Context
}

type ModelRuntimeRefreshOptions struct {
	AllowNetwork bool
	Force        bool
}

type DynamicModelProvider interface {
	ID() string
	StaticModels() []*Model
	RefreshModels(ctx ModelRefreshContext) ([]*Model, error)
}

type StaticModelProvider struct {
	Provider Provider
	Models   []*Model
}

func (p StaticModelProvider) ID() string             { return string(p.Provider) }
func (p StaticModelProvider) StaticModels() []*Model { return cloneModels(p.Models) }
func (p StaticModelProvider) RefreshModels(ModelRefreshContext) ([]*Model, error) {
	return cloneModels(p.Models), nil
}

type ModelRuntime struct {
	mu        sync.RWMutex
	store     ModelsStore
	providers map[Provider]DynamicModelProvider
	models    map[Provider][]*Model
	inflight  map[Provider]*modelRefreshCall
}

type modelRefreshCall struct {
	done chan struct{}
	err  error
}

type ModelRuntimeRefreshResult struct {
	Aborted bool
	Errors  map[Provider]error
}

func NewModelRuntime(store ModelsStore) *ModelRuntime {
	if store == nil {
		store = NewInMemoryModelsStore()
	}
	return &ModelRuntime{store: store, providers: map[Provider]DynamicModelProvider{}, models: map[Provider][]*Model{}, inflight: map[Provider]*modelRefreshCall{}}
}

func (r *ModelRuntime) SetProvider(provider DynamicModelProvider) {
	if provider == nil || provider.ID() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := Provider(provider.ID())
	r.providers[id] = provider
	r.models[id] = cloneModels(provider.StaticModels())
}

func (r *ModelRuntime) GetModels(provider Provider) []*Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if provider != "" {
		return cloneModels(r.models[provider])
	}
	var out []*Model
	for _, models := range r.models {
		out = append(out, cloneModels(models)...)
	}
	return out
}

func (r *ModelRuntime) GetModel(provider Provider, id string) *Model {
	for _, model := range r.GetModels(provider) {
		if model.ID == id {
			return model
		}
	}
	return nil
}

func (r *ModelRuntime) Refresh(ctx context.Context, allowNetwork bool) ModelRuntimeRefreshResult {
	return r.RefreshWithOptions(ctx, ModelRuntimeRefreshOptions{AllowNetwork: allowNetwork})
}

func (r *ModelRuntime) RefreshWithOptions(ctx context.Context, opts ModelRuntimeRefreshOptions) ModelRuntimeRefreshResult {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	providers := make([]DynamicModelProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}
	r.mu.RUnlock()

	result := ModelRuntimeRefreshResult{Errors: map[Provider]error{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, provider := range providers {
		if ctx.Err() != nil {
			result.Aborted = true
			break
		}
		provider := provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.refreshProvider(ctx, provider, opts.AllowNetwork, opts.Force); err != nil && ctx.Err() == nil {
				mu.Lock()
				result.Errors[Provider(provider.ID())] = err
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		result.Aborted = true
	}
	return result
}

func (r *ModelRuntime) refreshProvider(ctx context.Context, provider DynamicModelProvider, allowNetwork bool, force bool) error {
	id := Provider(provider.ID())
	r.mu.Lock()
	if call := r.inflight[id]; call != nil {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-call.done:
			return call.err
		}
	}
	call := &modelRefreshCall{done: make(chan struct{})}
	r.inflight[id] = call
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.inflight, id)
		r.mu.Unlock()
		close(call.done)
	}()

	stored, storeErr := r.store.Read(id)
	if storeErr == nil && stored != nil {
		r.setProviderModels(id, filterProviderModels(id, stored.Models))
	}
	if !allowNetwork || ctx.Err() != nil {
		call.err = ctx.Err()
		return call.err
	}
	models, err := provider.RefreshModels(ModelRefreshContext{Provider: id, Store: r.store, AllowNetwork: allowNetwork, Force: force, Signal: ctx})
	if err != nil {
		call.err = err
		return err
	}
	models = filterProviderModels(id, models)
	r.setProviderModels(id, models)
	if err := r.store.Write(id, &ModelsStoreEntry{Models: models, CheckedAt: time.Now().UnixMilli()}); err != nil {
		call.err = err
		return err
	}
	return nil
}

func (r *ModelRuntime) setProviderModels(provider Provider, models []*Model) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[provider] = cloneModels(models)
}

func filterProviderModels(provider Provider, models []*Model) []*Model {
	out := make([]*Model, 0, len(models))
	for _, model := range models {
		if model != nil && model.Provider == provider {
			out = append(out, cloneModel(model))
		}
	}
	return out
}

func RegisterRuntimeModels(runtime *ModelRuntime) error {
	if runtime == nil {
		return fmt.Errorf("nil model runtime")
	}
	modelRuntimeMu.Lock()
	defaultRuntime = runtime
	modelRuntimeMu.Unlock()
	syncLegacyModelRegistry(runtime.GetModels(""))
	return nil
}

// RegisterDynamicModelProvider installs a production dynamic model provider in
// the package default runtime. Package-level GetModel/ListModels observe its
// models immediately and after RefreshModels.
func RegisterDynamicModelProvider(provider DynamicModelProvider) {
	if provider == nil || provider.ID() == "" {
		return
	}
	modelRuntimeMu.RLock()
	runtime := defaultRuntime
	modelRuntimeMu.RUnlock()
	runtime.SetProvider(provider)
	syncLegacyModelRegistry(runtime.GetModels(""))
}

// RefreshModels refreshes the package default runtime and synchronizes the
// legacy registry snapshot used by older callers.
func RefreshModels(ctx context.Context, allowNetwork bool) ModelRuntimeRefreshResult {
	return RefreshModelsWithOptions(ctx, ModelRuntimeRefreshOptions{AllowNetwork: allowNetwork})
}

func RefreshModelsWithOptions(ctx context.Context, opts ModelRuntimeRefreshOptions) ModelRuntimeRefreshResult {
	modelRuntimeMu.RLock()
	runtime := defaultRuntime
	modelRuntimeMu.RUnlock()
	result := runtime.RefreshWithOptions(ctx, opts)
	syncLegacyModelRegistry(runtime.GetModels(""))
	return result
}

func syncLegacyModelRegistry(all []*Model) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	for k := range models {
		delete(models, k)
	}
	for _, model := range all {
		if model != nil && model.Provider != "" && model.ID != "" {
			models[string(model.Provider)+"/"+model.ID] = cloneModel(model)
		}
	}
}

func cloneModelsStoreEntry(entry *ModelsStoreEntry) *ModelsStoreEntry {
	if entry == nil {
		return nil
	}
	return &ModelsStoreEntry{Models: cloneModels(entry.Models), CheckedAt: entry.CheckedAt, LastModified: entry.LastModified, ETag: entry.ETag}
}

func cloneModels(models []*Model) []*Model {
	out := make([]*Model, 0, len(models))
	for _, model := range models {
		out = append(out, cloneModel(model))
	}
	return out
}

func cloneModel(model *Model) *Model {
	if model == nil {
		return nil
	}
	copy := *model
	copy.Input = append([]string{}, model.Input...)
	if model.Headers != nil {
		copy.Headers = map[string]string{}
		for k, v := range model.Headers {
			copy.Headers[k] = v
		}
	}
	return &copy
}
