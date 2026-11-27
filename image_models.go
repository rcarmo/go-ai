package goai

import (
	"sort"
	"sync"
)

const (
	ImageApiOpenRouter      ImageApi      = "openrouter-images"
	ImageApiPiImage         ImageApi      = "pi-image"
	ImageProviderOpenRouter ImageProvider = "openrouter"
)

var (
	imageModelRegistryMu sync.RWMutex
	imageModelRegistry   = map[string]*ImageModel{}
)

func ClearImageModels() {
	imageModelRegistryMu.Lock()
	defer imageModelRegistryMu.Unlock()
	for key := range imageModelRegistry {
		delete(imageModelRegistry, key)
	}
}

func RegisterImageModel(m *ImageModel) {
	if m == nil || m.Provider == "" || m.ID == "" {
		return
	}
	imageModelRegistryMu.Lock()
	defer imageModelRegistryMu.Unlock()
	imageModelRegistry[string(m.Provider)+"/"+m.ID] = cloneImageModel(m)
}

func GetImageModel(provider ImageProvider, id string) *ImageModel {
	if entries, known := RuntimeImageCatalogs(provider)[provider]; known {
		for _, model := range entries {
			if model.ID == id {
				return model
			}
		}
		return nil
	}
	imageModelRegistryMu.RLock()
	defer imageModelRegistryMu.RUnlock()
	return cloneImageModel(imageModelRegistry[string(provider)+"/"+id])
}

func ListImageModels(provider ImageProvider) []*ImageModel {
	catalogs := RuntimeImageCatalogs(provider)
	out := []*ImageModel{}
	for _, models := range catalogs {
		out = append(out, models...)
	}
	imageModelRegistryMu.RLock()
	defer imageModelRegistryMu.RUnlock()
	for _, model := range imageModelRegistry {
		if _, overridden := catalogs[model.Provider]; !overridden && (provider == "" || model.Provider == provider) {
			out = append(out, cloneImageModel(model))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider == out[j].Provider {
			return out[i].ID < out[j].ID
		}
		return out[i].Provider < out[j].Provider
	})
	return out
}

func ListImageProviders() []ImageProvider {
	seen := map[ImageProvider]bool{}
	for _, model := range ListImageModels("") {
		seen[model.Provider] = true
	}
	out := make([]ImageProvider, 0, len(seen))
	for provider := range seen {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func cloneImageModel(model *ImageModel) *ImageModel {
	if model == nil {
		return nil
	}
	copy := *model
	copy.InputLimits = cloneModelInputLimits(model.InputLimits)
	if rate := model.Cost.ImageOutput; rate != nil {
		copy.Cost.ImageOutput = new(*rate)
	}
	copy.Cost.Tiers = append([]ModelCostTier(nil), model.Cost.Tiers...)
	copy.Input = append([]string{}, model.Input...)
	copy.Output = append([]string{}, model.Output...)
	if model.Headers != nil {
		copy.Headers = map[string]string{}
		for key, value := range model.Headers {
			copy.Headers[key] = value
		}
	}
	return &copy
}
