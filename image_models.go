package goai

import (
	"sort"
	"sync"
)

const (
	ImageApiOpenRouter      ImageApi      = "openrouter-images"
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
	imageModelRegistryMu.RLock()
	defer imageModelRegistryMu.RUnlock()
	return cloneImageModel(imageModelRegistry[string(provider)+"/"+id])
}

func ListImageModels(provider ImageProvider) []*ImageModel {
	imageModelRegistryMu.RLock()
	defer imageModelRegistryMu.RUnlock()
	out := []*ImageModel{}
	for _, model := range imageModelRegistry {
		if provider == "" || model.Provider == provider {
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
	imageModelRegistryMu.RLock()
	defer imageModelRegistryMu.RUnlock()
	seen := map[ImageProvider]bool{}
	for _, model := range imageModelRegistry {
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
	copy.Input = append([]string{}, model.Input...)
	if model.Headers != nil {
		copy.Headers = map[string]string{}
		for key, value := range model.Headers {
			copy.Headers[key] = value
		}
	}
	return &copy
}
