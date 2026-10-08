package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// ModelCatalog is the credential-scoped model runtime supplied by the host.
// Resolution, discovery and refresh are shared by Harness, tasks, tools/hooks.
// It never serialises credentials, transport endpoints or callbacks into tasks.
type ModelCatalog interface {
	GetModel(goai.Provider, string) *goai.Model
	GetModels(goai.Provider) []*goai.Model
	RefreshWithOptions(context.Context, goai.ModelRuntimeRefreshOptions) goai.ModelRuntimeRefreshResult
}
type ModelAccess struct{ runtime *TaskRuntime }

func (r *TaskRuntime) Models() (*ModelAccess, error) {
	if e := r.check(); e != nil {
		return nil, e
	}
	return &ModelAccess{runtime: r}, nil
}
func (a *ModelAccess) GetModel(provider goai.Provider, id string) (*goai.Model, error) {
	if a == nil || a.runtime == nil {
		return nil, ErrSealed
	}
	r := a.runtime
	if e := r.check(); e != nil {
		return nil, e
	}
	model := r.harness.options.Models(provider, id)
	if e := r.check(); e != nil {
		return nil, e
	}
	if model == nil {
		return nil, nil
	}
	return cloneModel(model, r.harness.session.limits)
}
func (a *ModelAccess) GetModels(provider goai.Provider) ([]*goai.Model, error) {
	if a == nil || a.runtime == nil {
		return nil, ErrSealed
	}
	r := a.runtime
	if e := r.check(); e != nil {
		return nil, e
	}
	var models []*goai.Model
	if r.harness.options.Catalog != nil {
		models = r.harness.options.Catalog.GetModels(provider)
	} else {
		models = goai.ListModels(provider)
	}
	out := make([]*goai.Model, 0, len(models))
	for _, m := range models {
		owned, e := cloneModel(m, r.harness.session.limits)
		if e != nil {
			return nil, e
		}
		out = append(out, owned)
	}
	if e := r.check(); e != nil {
		return nil, e
	}
	return out, nil
}
func (a *ModelAccess) Refresh(ctx context.Context, options goai.ModelRuntimeRefreshOptions) (goai.ModelRuntimeRefreshResult, error) {
	if a == nil || a.runtime == nil {
		return goai.ModelRuntimeRefreshResult{}, ErrSealed
	}
	r := a.runtime
	if e := r.check(); e != nil {
		return goai.ModelRuntimeRefreshResult{}, e
	}
	if ctx == nil {
		return goai.ModelRuntimeRefreshResult{}, reject("nil context")
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.context, cancel)
	defer func() { stop(); cancel() }()
	var result goai.ModelRuntimeRefreshResult
	if r.harness.options.Catalog != nil {
		result = r.harness.options.Catalog.RefreshWithOptions(linked, options)
	} else {
		result = goai.RefreshModelsWithOptions(linked, options)
	}
	if e := r.check(); e != nil {
		return result, e
	}
	return result, linked.Err()
}

// Complete uses the same model and process-local credential/request boundary
// as generation. It never exposes API keys or endpoints in catalog records.
func (a *ModelAccess) Complete(ctx context.Context, ref ModelRef, input *goai.Context) (*goai.Message, error) {
	if a == nil || a.runtime == nil {
		return nil, ErrSealed
	}
	r := a.runtime
	if err := r.check(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, reject("nil context")
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.context, cancel)
	defer func() { stop(); cancel() }()
	model := r.harness.options.Models(ref.Provider, ref.ID)
	if model == nil {
		return nil, reject("model unavailable")
	}
	var options *goai.StreamOptions
	var err error
	if r.harness.options.RequestOptions != nil {
		options, err = callRequestOptions(r.harness.options.RequestOptions, linked, ref)
		if err != nil {
			return nil, err
		}
		options, err = cloneOptions(options, r.harness.session.limits)
		if err != nil {
			return nil, err
		}
	}
	if err = r.check(); err != nil {
		return nil, err
	}
	message, err := goai.Complete(linked, model, input, options)
	if lifetimeErr := r.check(); lifetimeErr != nil {
		return message, lifetimeErr
	}
	return message, err
}

func (a *ToolAPI) Models() (*ModelAccess, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return nil, ErrSealed
	}
	return a.runtime.Models()
}
