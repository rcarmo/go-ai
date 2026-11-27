package goai

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ModelsStoreEntry is a provider-scoped cached dynamic model catalog.
type ModelsStoreEntry struct {
	Models []*Model
	// Nil typed lists represent a legacy chat-only snapshot; an empty list
	// replaces the corresponding provider catalog with no models.
	ImageModels      []*ImageModel
	ClassifierModels []*ClassifierModel
	CheckedAt        int64
	LastModified     int64
	ETag             string
	// OwnerFingerprint scopes credential-derived catalogs without persisting
	// an access or refresh token (or a hash of either token).
	OwnerFingerprint string
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
	BaseURL      string
	Provider     Provider
	Store        ModelsStore
	AllowNetwork bool
	// Force asks dynamic providers to bypass their own freshness checks and fetch immediately.
	Force  bool
	Signal context.Context
}

type ModelRuntimeRefreshOptions struct {
	// AllowNetwork permits fetching and supersedes an in-flight refresh.
	// Cache-only refreshes may join existing work but never start a fetch.
	AllowNetwork bool
	// Force bypasses provider-specific freshness checks when fetching.
	Force bool
}

type DynamicModelProvider interface {
	ID() string
	StaticModels() []*Model
	RefreshModels(ctx ModelRefreshContext) ([]*Model, error)
}

// DynamicModelEndpointProvider exposes a registered endpoint override for refresh.
type DynamicModelEndpointProvider interface{ BaseURL() string }

type DynamicModelEntryProvider interface {
	DynamicModelProvider
	RefreshModelEntry(ctx ModelRefreshContext) (*ModelsStoreEntry, error)
}

// DynamicModelStaticEntryProvider exposes a credential-derived initial catalog.
type DynamicModelStaticEntryProvider interface {
	StaticModelEntry() *ModelsStoreEntry
}

// DynamicModelCacheProvider admits credential-scoped cache entries before
// either validators or catalog rows are reused.
type DynamicModelCacheProvider interface {
	OwnsModelCacheEntry(entry *ModelsStoreEntry) bool
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
	catalogs  map[Provider]*ModelsStoreEntry
	refreshes map[Provider]*modelRefreshState
}

type modelRefreshState struct {
	current *modelRefreshCall // guarded by ModelRuntime.mu
	publish sync.Mutex        // orders cache restoration and writes for this provider
}

type modelRefreshCall struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

type ModelRuntimeRefreshResult struct {
	Aborted bool
	Errors  map[Provider]error
}

func NewModelRuntime(store ModelsStore) *ModelRuntime {
	if store == nil {
		store = NewInMemoryModelsStore()
	}
	return &ModelRuntime{store: store, providers: map[Provider]DynamicModelProvider{}, catalogs: map[Provider]*ModelsStoreEntry{}, refreshes: map[Provider]*modelRefreshState{}}
}

func (r *ModelRuntime) SetProvider(provider DynamicModelProvider) {
	if provider == nil || provider.ID() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id := Provider(provider.ID())
	r.providers[id] = provider
	var entry *ModelsStoreEntry
	if typed, ok := provider.(DynamicModelStaticEntryProvider); ok {
		entry = typed.StaticModelEntry()
	}
	if entry == nil {
		entry = &ModelsStoreEntry{Models: provider.StaticModels()}
	}
	r.catalogs[id] = filterProviderCatalog(id, entry)
}

func (r *ModelRuntime) GetModels(provider Provider) []*Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if provider != "" {
		if entry := r.catalogs[provider]; entry != nil {
			return cloneModels(entry.Models)
		}
		return nil
	}
	var out []*Model
	for _, entry := range r.catalogs {
		out = append(out, cloneModels(entry.Models)...)
	}
	return out
}

func (r *ModelRuntime) GetModel(provider Provider, id string) *Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if provider != "" {
		entry := r.catalogs[provider]
		if entry == nil {
			return nil
		}
		for _, model := range entry.Models {
			if model != nil && model.ID == id {
				return cloneModel(model)
			}
		}
		return nil
	}
	for _, entry := range r.catalogs {
		for _, model := range entry.Models {
			if model != nil && model.ID == id {
				return cloneModel(model)
			}
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
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return err
	}
	state := r.refreshes[id]
	if state == nil {
		state = &modelRefreshState{}
		r.refreshes[id] = state
	}
	previous := state.current
	if previous != nil && !allowNetwork {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-previous.done:
			return previous.err
		}
	}
	workCtx, cancel := context.WithCancel(ctx)
	call := &modelRefreshCall{cancel: cancel, done: make(chan struct{})}
	state.current = call
	if previous != nil {
		previous.cancel()
	}
	r.mu.Unlock()

	defer func() {
		cancel()
		r.mu.Lock()
		if state.current == call {
			state.current = nil
		}
		r.mu.Unlock()
		close(call.done)
	}()

	// A provider need not honor its signal. Let the caller return first and
	// allow the worker to deliver its eventual result without a receiver.
	result := make(chan error, 1)
	go func() {
		result <- r.runProviderRefresh(workCtx, id, state, call, provider, allowNetwork, force)
	}()
	select {
	case <-workCtx.Done():
	case err := <-result:
		if workCtx.Err() == nil {
			call.err = err
			return call.err
		}
	}
	// Supersession, unlike caller cancellation, is not a refresh error.
	call.err = ctx.Err()
	return call.err
}

func (r *ModelRuntime) runProviderRefresh(ctx context.Context, id Provider, state *modelRefreshState, call *modelRefreshCall, provider DynamicModelProvider, allowNetwork bool, force bool) error {
	state.publish.Lock()
	if r.currentModelRefresh(ctx, state, call) {
		if stored, err := r.store.Read(id); err == nil && stored != nil && modelCacheEntryOwned(provider, stored) {
			r.setRefreshedCatalog(ctx, state, call, id, filterProviderCatalog(id, stored))
		}
	}
	state.publish.Unlock()
	if !allowNetwork || ctx.Err() != nil {
		return nil
	}

	refreshCtx := ModelRefreshContext{Provider: id, BaseURL: modelRefreshBaseURL(provider), Store: r.store, AllowNetwork: allowNetwork, Force: force, Signal: ctx}
	var entry *ModelsStoreEntry
	var err error
	if entryProvider, ok := provider.(DynamicModelEntryProvider); ok {
		entry, err = entryProvider.RefreshModelEntry(refreshCtx)
	} else {
		var models []*Model
		models, err = provider.RefreshModels(refreshCtx)
		if err == nil {
			entry = &ModelsStoreEntry{Models: models, CheckedAt: time.Now().UnixMilli()}
		}
	}
	if err != nil {
		return NewModelsError(ModelsErrorModelSource, fmt.Sprintf("model refresh failed for %s", id), err)
	}
	if entry == nil {
		entry = &ModelsStoreEntry{CheckedAt: time.Now().UnixMilli()}
	}
	entry = filterProviderCatalog(id, entry)
	state.publish.Lock()
	defer state.publish.Unlock()
	if !r.currentModelRefresh(ctx, state, call) {
		return nil
	}
	// Serialize writes independently of network work. If superseded during
	// an already-started store write, skip its in-memory update as well.
	err = r.store.Write(id, cloneModelsStoreEntry(entry))
	r.setRefreshedCatalog(ctx, state, call, id, entry)
	if err != nil {
		return NewModelsError(ModelsErrorModelSource, fmt.Sprintf("model store write failed for %s", id), err)
	}
	return nil
}

func (r *ModelRuntime) currentModelRefresh(ctx context.Context, state *modelRefreshState, call *modelRefreshCall) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return state.current == call && ctx.Err() == nil
}

func (r *ModelRuntime) setRefreshedCatalog(ctx context.Context, state *modelRefreshState, call *modelRefreshCall, provider Provider, entry *ModelsStoreEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state.current == call && ctx.Err() == nil {
		r.catalogs[provider] = cloneModelsStoreEntry(entry)
	}
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

// registerBuiltinModelProviders publishes the legacy snapshot once, after all
// built-in providers have been installed. SetProvider retains its normal deep
// copy boundary; unrelated dynamic providers remain registered.
func registerBuiltinModelProviders(byProvider map[Provider][]*Model) {
	modelRuntimeMu.RLock()
	runtime := defaultRuntime
	modelRuntimeMu.RUnlock()
	for provider, models := range byProvider {
		runtime.SetProvider(StaticModelProvider{Provider: provider, Models: models})
	}
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
	out := *entry
	out.Models = cloneModels(entry.Models)
	out.ImageModels = copyCatalogModels(entry.ImageModels, cloneImageModel, nil)
	out.ClassifierModels = copyCatalogModels(entry.ClassifierModels, cloneClassifierModel, nil)
	return &out
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
	if rate := model.Cost.ImageOutput; rate != nil {
		copy.Cost.ImageOutput = new(*rate)
	}
	copy.Cost.Tiers = append([]ModelCostTier(nil), model.Cost.Tiers...)
	copy.Input = append([]string{}, model.Input...)
	copy.InputLimits = cloneModelInputLimits(model.InputLimits)
	if model.MistralCompat != nil {
		compat := *model.MistralCompat
		copy.MistralCompat = &compat
	}
	if model.GoogleCompat != nil {
		compat := *model.GoogleCompat
		if compat.SupportsStrictMode != nil {
			enabled := *compat.SupportsStrictMode
			compat.SupportsStrictMode = &enabled
		}
		copy.GoogleCompat = &compat
	}
	if model.BedrockCompat != nil {
		compat := *model.BedrockCompat
		if compat.SupportsStrictMode != nil {
			enabled := *compat.SupportsStrictMode
			compat.SupportsStrictMode = &enabled
		}
		if compat.SupportsReasoningReplay != nil {
			enabled := *compat.SupportsReasoningReplay
			compat.SupportsReasoningReplay = &enabled
		}
		copy.BedrockCompat = &compat
	}
	if model.PromptCache != nil {
		promptCache := *model.PromptCache
		copy.PromptCache = &promptCache
	}
	if model.Enabled != nil {
		enabled := *model.Enabled
		copy.Enabled = &enabled
	}
	if model.Free != nil {
		free := *model.Free
		copy.Free = &free
	}
	copy.Providers = append([]ModelProviderInfo{}, model.Providers...)
	copy.SamplingParams = cloneSamplingParams(model.SamplingParams)
	if model.SamplingParamsByThinkingLevel != nil {
		copy.SamplingParamsByThinkingLevel = make(map[ModelThinkingLevel]map[string]any, len(model.SamplingParamsByThinkingLevel))
		for level, params := range model.SamplingParamsByThinkingLevel {
			copy.SamplingParamsByThinkingLevel[level] = cloneSamplingParams(params)
		}
	}
	if model.Headers != nil {
		copy.Headers = map[string]string{}
		for k, v := range model.Headers {
			copy.Headers[k] = v
		}
	}
	return &copy
}

func modelRefreshBaseURL(provider DynamicModelProvider) string {
	if endpoint, ok := provider.(DynamicModelEndpointProvider); ok {
		return endpoint.BaseURL()
	}
	return ""
}

func modelCacheEntryOwned(provider DynamicModelProvider, entry *ModelsStoreEntry) bool {
	if owner, ok := provider.(DynamicModelCacheProvider); ok {
		return owner.OwnsModelCacheEntry(entry)
	}
	return entry != nil
}

func cloneModelInputLimits(input *ModelInputLimits) *ModelInputLimits {
	if input == nil {
		return nil
	}
	limits := *input
	if input.Images != nil {
		images := *input.Images
		if input.Images.Resize != nil {
			resize := *input.Images.Resize
			images.Resize = &resize
		}
		limits.Images = &images
	}
	return &limits
}
