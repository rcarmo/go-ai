package durable

import goai "github.com/rcarmo/go-ai"

func (a *ModelAccess) check() error {
	if a == nil || a.runtime == nil {
		return ErrSealed
	}
	return a.runtime.check()
}

// GetModelOfType returns a detached model DTO without auth headers/endpoints.
// Chat lookup uses the existing host boundary; typed lookup uses the optional
// scoped catalogue, or the global native registry only when no scope exists.
func (a *ModelAccess) GetModelOfType(kind goai.ModelType, provider goai.Provider, id string) (any, error) {
	if err := a.check(); err != nil {
		return nil, err
	}
	if kind == goai.ModelTypeChat {
		return a.GetModel(provider, id)
	}
	if kind != goai.ModelTypeImage && kind != goai.ModelTypeClassifier {
		return nil, reject("invalid model type")
	}
	var value any
	catalog := a.runtime.harness.options.Catalog
	if typed, ok := catalog.(TypedModelCatalog); ok {
		value = typed.GetTypedModel(goai.TypedModelRef{Type: kind, Provider: string(provider), ID: id})
	} else if catalog == nil {
		value = goai.GetTypedModel(goai.TypedModelRef{Type: kind, Provider: string(provider), ID: id})
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	return cloneTypedModel(value, kind, a.runtime.harness.session.limits)
}
func (a *ModelAccess) GetModelsOfType(kind goai.ModelType, provider goai.Provider) ([]any, error) {
	if err := a.check(); err != nil {
		return nil, err
	}
	if kind == goai.ModelTypeChat {
		models, err := a.GetModels(provider)
		if err != nil {
			return nil, err
		}
		out := make([]any, len(models))
		for i, model := range models {
			out[i] = model
		}
		return out, nil
	}
	if kind != goai.ModelTypeImage && kind != goai.ModelTypeClassifier {
		return nil, reject("invalid model type")
	}
	values := []any{}
	catalog := a.runtime.harness.options.Catalog
	if typed, ok := catalog.(TypedModelCatalog); ok {
		values = typed.GetTypedModels(kind, provider)
	} else if catalog == nil {
		values = goai.ListTypedModels(kind, string(provider))
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		copy, err := cloneTypedModel(value, kind, a.runtime.harness.session.limits)
		if err != nil {
			return nil, err
		}
		if copy != nil {
			out = append(out, copy)
		}
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	return out, nil
}
func cloneTypedModel(value any, kind goai.ModelType, limits Limits) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch kind {
	case goai.ModelTypeImage:
		model, ok := value.(*goai.ImageModel)
		if !ok {
			return nil, reject("image catalogue returned wrong model type")
		}
		if model == nil {
			return nil, nil
		}
		copy := *model
		copy.BaseURL = ""
		copy.Headers = nil
		encoded, err := encodeBounded(copy, limits, limits.MaxRecordBytes)
		if err != nil {
			return nil, err
		}
		var owned goai.ImageModel
		if err = decodeStrict(encoded, limits, limits.MaxRecordBytes, &owned); err != nil {
			return nil, err
		}
		if goai.GetModelType(&owned) != kind {
			return nil, reject("image catalogue returned wrong discriminator")
		}
		return &owned, nil
	case goai.ModelTypeClassifier:
		model, ok := value.(*goai.ClassifierModel)
		if !ok {
			return nil, reject("classifier catalogue returned wrong model type")
		}
		if model == nil {
			return nil, nil
		}
		copy := *model
		copy.BaseURL = ""
		copy.Headers = nil
		encoded, err := encodeBounded(copy, limits, limits.MaxRecordBytes)
		if err != nil {
			return nil, err
		}
		var owned goai.ClassifierModel
		if err = decodeStrict(encoded, limits, limits.MaxRecordBytes, &owned); err != nil {
			return nil, err
		}
		if goai.GetModelType(&owned) != kind {
			return nil, reject("classifier catalogue returned wrong discriminator")
		}
		return &owned, nil
	default:
		return nil, reject("invalid model type")
	}
}
