package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// Deferred cancellation is best effort host work of the fresh abort invocation.
// Close leaves the handle pending for reopen. Provider failures are reported but
// cannot erase the durable abort mark or prevent local bottom-up settlement.
func (h *Harness) cancelDeferred(ctx context.Context, cp generationCheckpoint) {
	if cp.Phase != "poll" || cp.Deferred == nil || cp.Model == nil {
		return
	}
	provider := goai.GetApiProvider(cp.Model.Api)
	if provider == nil || provider.CancelDeferred == nil {
		return
	}
	local, err := callModelResolver(h.options.Models, cp.Agent.Model)
	if err != nil || local == nil {
		if err == nil {
			err = reject("deferred cancellation model unavailable")
		}
		h.scheduler.report(err)
		return
	}
	model, err := cloneModel(cp.Model, h.session.limits)
	if err != nil {
		h.scheduler.report(err)
		return
	}
	model.BaseURL, model.APIKey = local.BaseURL, local.APIKey
	model.Headers = make(map[string]string, len(local.Headers))
	for key, value := range local.Headers {
		model.Headers[key] = value
	}
	var options *goai.StreamOptions
	if h.options.RequestOptions != nil {
		options, err = callRequestOptions(h.options.RequestOptions, ctx, cp.Agent.Model)
		if err != nil {
			h.scheduler.report(err)
			return
		}
	}
	options, err = cloneOptions(options, h.session.limits)
	if err != nil {
		h.scheduler.report(err)
		return
	}
	options.Deferred = cp.Agent.Settings.Deferred
	err = callDeferredCancel(ctx, model, *cp.Deferred, options)
	if err != nil {
		h.scheduler.report(err)
	}
}
func callDeferredCancel(ctx context.Context, model *goai.Model, handle goai.DeferredHandle, options *goai.StreamOptions) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("deferred cancellation callback panic")
		}
	}()
	return goai.CancelDeferred(ctx, model, handle, options)
}
