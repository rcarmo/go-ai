package goai

import (
	"context"
	"fmt"
	"sync"
)

// --- Provider Registry ---

// ProviderStream is the function signature that each provider implements.
// It returns a channel of events that the caller reads until closed.
type ProviderStream func(ctx context.Context, model *Model, convCtx *Context, opts *StreamOptions) <-chan Event

// ApiProvider holds the stream implementations for a wire protocol.
type ApiProvider struct {
	Api          Api
	Stream       ProviderStream
	StreamSimple ProviderStream
}

var (
	registryMu sync.RWMutex
	apiProviders = map[Api]*ApiProvider{}
)

// RegisterApi registers a provider implementation for a wire protocol.
func RegisterApi(p *ApiProvider) {
	registryMu.Lock()
	defer registryMu.Unlock()
	apiProviders[p.Api] = p
}

// GetApiProvider returns the registered provider for an API, or nil.
func GetApiProvider(api Api) *ApiProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return apiProviders[api]
}

// --- Model Registry ---

var (
	modelsMu sync.RWMutex
	models   = map[string]*Model{} // key: "provider/id"
)

// RegisterModel adds a model to the global registry.
func RegisterModel(m *Model) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	models[string(m.Provider)+"/"+m.ID] = m
}

// GetModel retrieves a model by provider and ID.
// Returns nil if not found.
func GetModel(provider Provider, id string) *Model {
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	return models[string(provider)+"/"+id]
}

// ListModels returns all registered models, optionally filtered by provider.
func ListModels(provider Provider) []*Model {
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	var out []*Model
	for _, m := range models {
		if provider == "" || m.Provider == provider {
			out = append(out, m)
		}
	}
	return out
}

// ListProviders returns all provider names that have at least one model registered.
func ListProviders() []Provider {
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	seen := map[Provider]bool{}
	for _, m := range models {
		seen[m.Provider] = true
	}
	out := make([]Provider, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// --- Top-level API ---

// Stream starts a streaming LLM request and returns a channel of events.
// Read all events until the channel is closed. The final event is either
// DoneEvent or ErrorEvent.
func Stream(ctx context.Context, model *Model, convCtx *Context, opts *StreamOptions) <-chan Event {
	p := GetApiProvider(model.Api)
	if p == nil {
		ch := make(chan Event, 1)
		ch <- &ErrorEvent{
			Reason: StopReasonError,
			Err:    fmt.Errorf("no provider registered for API %q", model.Api),
		}
		close(ch)
		return ch
	}
	if opts != nil && opts.Reasoning != nil {
		return p.StreamSimple(ctx, model, convCtx, opts)
	}
	return p.Stream(ctx, model, convCtx, opts)
}

// Complete makes a non-streaming LLM request and returns the final message.
func Complete(ctx context.Context, model *Model, convCtx *Context, opts *StreamOptions) (*Message, error) {
	events := Stream(ctx, model, convCtx, opts)
	var result *Message
	var resultErr error
	for event := range events {
		switch e := event.(type) {
		case *DoneEvent:
			result = e.Message
		case *ErrorEvent:
			result = e.Error
			resultErr = e.Err
			if resultErr == nil {
				resultErr = fmt.Errorf("LLM error: %s (reason: %s)", result.ErrorMessage, e.Reason)
			}
		}
	}
	if resultErr != nil {
		return result, resultErr
	}
	return result, nil
}
