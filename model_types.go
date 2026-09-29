package goai

import (
	"fmt"
	"strings"
)

type ModelType string

const (
	ModelTypeChat       ModelType = "chat"
	ModelTypeImage      ModelType = "image"
	ModelTypeClassifier ModelType = "classifier"
)

type TypedModelRef struct {
	Type     ModelType `json:"type,omitempty"`
	Provider string    `json:"provider"`
	ID       string    `json:"id"`
}

func GetModelType(model any) ModelType {
	switch m := model.(type) {
	case *Model:
		if m != nil && m.Type != "" {
			return ModelType(m.Type)
		}
	case Model:
		if m.Type != "" {
			return ModelType(m.Type)
		}
	case *ImageModel:
		if m != nil && m.Type != "" {
			return ModelType(m.Type)
		}
		return ModelTypeImage
	case ImageModel:
		if m.Type != "" {
			return ModelType(m.Type)
		}
		return ModelTypeImage
	case *ClassifierModel:
		if m != nil && m.Type != "" {
			return ModelType(m.Type)
		}
		return ModelTypeClassifier
	case ClassifierModel:
		if m.Type != "" {
			return ModelType(m.Type)
		}
		return ModelTypeClassifier
	}
	return ModelTypeChat
}

func IsModelType(model any, typ ModelType) bool { return GetModelType(model) == typ }

func ParseTypedModelRef(s string) (TypedModelRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return TypedModelRef{}, fmt.Errorf("empty model reference")
	}
	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 2 {
		ref, err := parseProviderID(parts[1])
		if err != nil {
			return TypedModelRef{}, err
		}
		ref.Type = ModelType(parts[0])
		return ref, nil
	}
	return parseProviderID(s)
}

func parseProviderID(s string) (TypedModelRef, error) {
	provider, id, ok := strings.Cut(s, "/")
	if !ok || provider == "" || id == "" {
		return TypedModelRef{}, fmt.Errorf("model reference %q must be provider/id", s)
	}
	return TypedModelRef{Type: ModelTypeChat, Provider: provider, ID: id}, nil
}

func GetTypedModel(ref TypedModelRef) any {
	switch ref.Type {
	case "", ModelTypeChat:
		return GetModel(Provider(ref.Provider), ref.ID)
	case ModelTypeImage:
		return GetImageModel(ImageProvider(ref.Provider), ref.ID)
	case ModelTypeClassifier:
		return GetClassifierModel(ClassifierProvider(ref.Provider), ref.ID)
	default:
		return nil
	}
}

func ListTypedModels(typ ModelType, provider string) []any {
	switch typ {
	case "", ModelTypeChat:
		models := ListModels(Provider(provider))
		out := make([]any, 0, len(models))
		for _, model := range models {
			out = append(out, model)
		}
		return out
	case ModelTypeImage:
		models := ListImageModels(ImageProvider(provider))
		out := make([]any, 0, len(models))
		for _, model := range models {
			out = append(out, model)
		}
		return out
	case ModelTypeClassifier:
		models := ListClassifierModels(ClassifierProvider(provider))
		out := make([]any, 0, len(models))
		for _, model := range models {
			out = append(out, model)
		}
		return out
	default:
		return nil
	}
}
