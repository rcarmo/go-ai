package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

func (a *ModelAccess) HasAuth(ctx context.Context, ref goai.TypedModelRef) (bool, error) {
	if err := a.check(); err != nil {
		return false, err
	}
	if ctx == nil {
		return false, reject("nil auth context")
	}
	if ref.Type == "" {
		ref.Type = goai.ModelTypeChat
	}
	if !validModelType(ref.Type) || ref.Provider == "" || ref.ID == "" {
		return false, reject("invalid model auth reference")
	}
	// Resolve within the same host scope before asking its auth capability.
	model, err := a.GetModelOfType(ref.Type, goai.Provider(ref.Provider), ref.ID)
	if err != nil {
		return false, err
	}
	if model == nil {
		return false, nil
	}
	catalog, ok := a.runtime.harness.options.Catalog.(ModelAuthCatalog)
	if !ok {
		return false, ErrUnsupported
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.runtime.context, cancel)
	defer func() { stop(); cancel() }()
	authenticated, err := catalog.HasModelAuth(linked, ref)
	if err != nil {
		return false, err
	}
	if err = a.check(); err != nil {
		return false, err
	}
	if err = linked.Err(); err != nil {
		return false, err
	}
	return authenticated, nil
}
func (a *ModelAccess) GetAvailableOfType(ctx context.Context, kind goai.ModelType, provider goai.Provider) ([]any, error) {
	if err := a.check(); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, reject("nil availability context")
	}
	if !validModelType(kind) {
		return nil, reject("invalid model type")
	}
	catalog, ok := a.runtime.harness.options.Catalog.(ModelAuthCatalog)
	if !ok {
		return nil, ErrUnsupported
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.runtime.context, cancel)
	defer func() { stop(); cancel() }()
	models, err := catalog.GetAvailableModels(linked, kind, provider)
	if err != nil {
		return nil, err
	}
	if len(models) > a.runtime.harness.session.limits.MaxPage {
		return nil, reject("available model collection limit")
	}
	out := make([]any, 0, len(models))
	for _, model := range models {
		var ref goai.TypedModelRef
		switch v := model.(type) {
		case *goai.Model:
			if v == nil {
				continue
			}
			ref = goai.TypedModelRef{Type: goai.ModelTypeChat, Provider: string(v.Provider), ID: v.ID}
		case *goai.ImageModel:
			if v == nil {
				continue
			}
			ref = goai.TypedModelRef{Type: goai.ModelTypeImage, Provider: string(v.Provider), ID: v.ID}
		case *goai.ClassifierModel:
			if v == nil {
				continue
			}
			ref = goai.TypedModelRef{Type: goai.ModelTypeClassifier, Provider: string(v.Provider), ID: v.ID}
		default:
			return nil, reject("availability returned invalid model type")
		}
		if ref.Type != kind || provider != "" && ref.Provider != string(provider) {
			return nil, reject("availability returned wrong scope/type")
		}
		scoped, err := a.GetModelOfType(kind, goai.Provider(ref.Provider), ref.ID)
		if err != nil {
			return nil, err
		}
		if scoped == nil {
			return nil, reject("availability widened catalogue scope")
		}
		var owned any
		if kind == goai.ModelTypeChat {
			owned, err = cloneModel(model.(*goai.Model), a.runtime.harness.session.limits)
		} else {
			owned, err = cloneTypedModel(model, kind, a.runtime.harness.session.limits)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, owned)
	}
	if err = a.check(); err != nil {
		return nil, err
	}
	if err = linked.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (a *ModelAccess) GetAvailable(ctx context.Context, provider goai.Provider) ([]*goai.Model, error) {
	models, err := a.GetAvailableOfType(ctx, goai.ModelTypeChat, provider)
	if err != nil {
		return nil, err
	}
	out := make([]*goai.Model, len(models))
	for i, model := range models {
		out[i] = model.(*goai.Model)
	}
	return out, nil
}
func validModelType(kind goai.ModelType) bool {
	return kind == goai.ModelTypeChat || kind == goai.ModelTypeImage || kind == goai.ModelTypeClassifier
}
