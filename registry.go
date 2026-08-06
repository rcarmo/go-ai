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

// ProviderFetchDeferred fetches or polls a provider-side deferred/background response.
type ProviderFetchDeferred func(ctx context.Context, model *Model, handle DeferredHandle, opts *StreamOptions) <-chan Event

// ProviderCancelDeferred cancels a provider-side deferred/background response.
type ProviderCancelDeferred func(ctx context.Context, model *Model, handle DeferredHandle, opts *StreamOptions) error

// ApiProvider holds stream and deferred-response implementations for a wire protocol.
type ApiProvider struct {
	Api            Api
	Stream         ProviderStream
	StreamSimple   ProviderStream
	FetchDeferred  ProviderFetchDeferred
	CancelDeferred ProviderCancelDeferred
}

var (
	registryMu   sync.RWMutex
	apiProviders = map[Api]*ApiProvider{}
)

// RegisterApi registers a provider implementation for a wire protocol.
func RegisterApi(p *ApiProvider) {
	if p == nil || p.Api == "" {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	apiProviders[p.Api] = p
}

// UnregisterApi removes a provider by API name.
func UnregisterApi(api Api) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(apiProviders, api)
}

// ClearApiProviders removes all registered API providers.
func ClearApiProviders() {
	registryMu.Lock()
	defer registryMu.Unlock()
	for k := range apiProviders {
		delete(apiProviders, k)
	}
}

// GetApiProvider returns the registered provider for an API, or nil.
func GetApiProvider(api Api) *ApiProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return apiProviders[api]
}

// ClearModels removes all registered models.
func ClearModels() {
	modelsMu.Lock()
	for k := range models {
		delete(models, k)
	}
	modelsMu.Unlock()

	modelRuntimeMu.Lock()
	defaultRuntime = NewModelRuntime(nil)
	modelRuntimeMu.Unlock()
}

// --- Model Registry ---

var (
	modelsMu       sync.RWMutex
	models         = map[string]*Model{} // key: "provider/id"
	modelRuntimeMu sync.RWMutex
	defaultRuntime = NewModelRuntime(nil)
)

// RegisterModel adds a model to the global registry.
func RegisterModel(m *Model) {
	if m == nil || m.Provider == "" || m.ID == "" {
		return
	}
	modelsMu.Lock()
	models[string(m.Provider)+"/"+m.ID] = m
	modelsMu.Unlock()

	modelRuntimeMu.RLock()
	runtime := defaultRuntime
	modelRuntimeMu.RUnlock()
	runtime.SetProvider(StaticModelProvider{Provider: m.Provider, Models: append(runtime.GetModels(m.Provider), m)})
}

// GetModel retrieves a model by provider and ID.
// Returns nil if not found.
func GetModel(provider Provider, id string) *Model {
	modelRuntimeMu.RLock()
	runtime := defaultRuntime
	modelRuntimeMu.RUnlock()
	if model := runtime.GetModel(provider, id); model != nil {
		return model
	}
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	return models[string(provider)+"/"+id]
}

// ListModels returns all registered models, optionally filtered by provider.
func ListModels(provider Provider) []*Model {
	modelRuntimeMu.RLock()
	runtime := defaultRuntime
	modelRuntimeMu.RUnlock()
	out := runtime.GetModels(provider)
	if len(out) != 0 || provider != "" {
		return out
	}
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	for _, m := range models {
		out = append(out, m)
	}
	return out
}

// ListProviders returns all provider names that have at least one model registered.
func ListProviders() []Provider {
	seen := map[Provider]bool{}
	for _, m := range ListModels("") {
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
	if model == nil {
		ch := make(chan Event, 1)
		ch <- &ErrorEvent{Reason: StopReasonError, Err: fmt.Errorf("nil model")}
		close(ch)
		return ch
	}
	if convCtx == nil {
		convCtx = &Context{}
	}
	p := GetApiProvider(model.Api)
	if p == nil {
		logError("no provider registered", "api", model.Api, "provider", model.Provider, "model", model.ID)
		ch := make(chan Event, 1)
		ch <- &ErrorEvent{
			Reason: StopReasonError,
			Err:    fmt.Errorf("no provider registered for API %q", model.Api),
		}
		close(ch)
		return ch
	}
	logDebug("stream start", "provider", model.Provider, "model", model.ID, "api", model.Api,
		"messages", len(convCtx.Messages), "tools", len(convCtx.Tools))
	if opts != nil && opts.Reasoning != nil && p.StreamSimple != nil {
		return p.StreamSimple(ctx, model, convCtx, opts)
	}
	if p.Stream == nil {
		ch := make(chan Event, 1)
		ch <- &ErrorEvent{Reason: StopReasonError, Err: fmt.Errorf("no stream function registered for API %q", model.Api)}
		close(ch)
		return ch
	}
	return p.Stream(ctx, model, convCtx, opts)
}

// Complete makes a non-streaming LLM request and returns the final message.
//
// On error, the returned message may be nil if the provider did not attach a
// partial/error message to its ErrorEvent. Always check err before using the
// returned message.
func Complete(ctx context.Context, model *Model, convCtx *Context, opts *StreamOptions) (*Message, error) {
	events := Stream(ctx, model, convCtx, opts)
	var result *Message
	var resultErr error
	provider, modelID := Provider(""), ""
	if model != nil {
		provider, modelID = model.Provider, model.ID
	}
	for event := range events {
		switch e := event.(type) {
		case *DoneEvent:
			result = e.Message
			input, output := 0, 0
			stop := StopReason("")
			if result != nil {
				stop = result.StopReason
				if result.Usage != nil {
					input, output = result.Usage.Input, result.Usage.Output
				}
			}
			logInfo("complete done", "provider", provider, "model", modelID,
				"stop", stop, "input", input, "output", output)
		case *ErrorEvent:
			result = e.Error
			resultErr = e.Err
			if resultErr == nil {
				message := ""
				if result != nil {
					message = result.ErrorMessage
				}
				resultErr = fmt.Errorf("LLM error: %s (reason: %s)", message, e.Reason)
			}
			logError("complete error", "provider", provider, "model", modelID,
				"reason", e.Reason, "error", resultErr)
		}
	}
	if resultErr != nil {
		return result, resultErr
	}
	return result, nil
}

// FetchDeferred fetches or polls a provider-side deferred/background response.
// Provider failures are returned in-band as assistant messages with stopReason
// "error"; structural errors (nil model, unsupported API, cancellation) are
// returned as Go errors.
func FetchDeferred(ctx context.Context, model *Model, handle DeferredHandle, opts *StreamOptions) (*Message, error) {
	if model == nil {
		return nil, fmt.Errorf("nil model")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := GetApiProvider(model.Api)
	if p == nil {
		return nil, fmt.Errorf("no provider registered for API %q", model.Api)
	}
	if p.FetchDeferred == nil {
		return nil, fmt.Errorf("API %q does not support deferred responses", model.Api)
	}
	if handle.Provider != "" && handle.Provider != string(model.Provider) {
		return nil, fmt.Errorf("deferred handle provider %q does not match model provider %q", handle.Provider, model.Provider)
	}
	if handle.ModelID != "" && handle.ModelID != model.ID {
		return nil, fmt.Errorf("deferred handle modelId %q does not match model %q", handle.ModelID, model.ID)
	}
	if handle.Api != "" && handle.Api != string(model.Api) {
		return nil, fmt.Errorf("deferred handle api %q does not match model API %q", handle.Api, model.Api)
	}
	return drainDeferredEvents(ctx, p.FetchDeferred(ctx, model, handle, opts))
}

// CancelDeferred cancels a provider-side deferred/background response.
func CancelDeferred(ctx context.Context, model *Model, handle DeferredHandle, opts *StreamOptions) error {
	if model == nil {
		return fmt.Errorf("nil model")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p := GetApiProvider(model.Api)
	if p == nil {
		return fmt.Errorf("no provider registered for API %q", model.Api)
	}
	if p.CancelDeferred == nil {
		return fmt.Errorf("API %q cannot cancel deferred responses", model.Api)
	}
	return p.CancelDeferred(ctx, model, handle, opts)
}

func drainDeferredEvents(ctx context.Context, events <-chan Event) (*Message, error) {
	var result *Message
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, ok := <-events:
			if !ok {
				if result == nil {
					return nil, fmt.Errorf("deferred response stream ended without a terminal event")
				}
				return result, nil
			}
			switch e := event.(type) {
			case *DoneEvent:
				result = e.Message
			case *ErrorEvent:
				if e.Error != nil {
					result = e.Error
				} else if e.Err != nil {
					return nil, e.Err
				}
			}
		}
	}
}
