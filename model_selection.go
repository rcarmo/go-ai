package goai

import (
	"fmt"
	"sort"
	"strings"
)

// ModelRef is a stable UI/storage reference to a model.
type ModelRef struct {
	Provider Provider `json:"provider"`
	ID       string   `json:"id"`
}

// String returns provider/id, suitable for menus and persisted settings.
func (r ModelRef) String() string {
	if r.Provider == "" {
		return r.ID
	}
	if r.ID == "" {
		return string(r.Provider)
	}
	return string(r.Provider) + "/" + r.ID
}

// ParseModelRef parses "provider/id" references used by UI pickers and config.
func ParseModelRef(s string) (ModelRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ModelRef{}, fmt.Errorf("empty model reference")
	}
	provider, id, ok := strings.Cut(s, "/")
	if !ok || provider == "" || id == "" {
		return ModelRef{}, fmt.Errorf("model reference %q must be provider/id", s)
	}
	return ModelRef{Provider: Provider(provider), ID: id}, nil
}

// FindModelByRef returns the matching model from a caller-supplied model list.
// This is useful for OAuth-filtered runtime model lists that may differ from the
// global registry.
func FindModelByRef(models []*Model, ref ModelRef) *Model {
	for _, model := range models {
		if model != nil && model.Provider == ref.Provider && model.ID == ref.ID {
			return model
		}
	}
	return nil
}

// FilterModelsByProvider returns models for one provider. Empty provider returns
// all non-nil models.
func FilterModelsByProvider(models []*Model, provider Provider) []*Model {
	out := make([]*Model, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		if provider == "" || model.Provider == provider {
			out = append(out, model)
		}
	}
	return out
}

// SortModelsForUI sorts models by provider, display name, then ID for stable
// model picker output.
func SortModelsForUI(models []*Model) []*Model {
	out := append([]*Model(nil), models...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		an, bn := a.Name, b.Name
		if an == "" {
			an = a.ID
		}
		if bn == "" {
			bn = b.ID
		}
		if an != bn {
			return an < bn
		}
		return a.ID < b.ID
	})
	return out
}

// ModelPickerItems returns stable provider/id labels for a UI model picker.
func ModelPickerItems(models []*Model, provider Provider) []string {
	filtered := SortModelsForUI(FilterModelsByProvider(models, provider))
	items := make([]string, 0, len(filtered))
	for _, model := range filtered {
		if model == nil {
			continue
		}
		items = append(items, ModelRef{Provider: model.Provider, ID: model.ID}.String())
	}
	return items
}

// SwitchModel transforms the conversation for a newly selected model and returns
// a shallow-copied Context. Provider-specific ephemeral state such as redacted
// thinking blocks and incompatible image input is normalized by TransformMessages.
func SwitchModel(ctx *Context, model *Model) *Context {
	if ctx == nil {
		return &Context{}
	}
	out := *ctx
	out.Messages = TransformMessages(ctx.Messages, model)
	return &out
}
