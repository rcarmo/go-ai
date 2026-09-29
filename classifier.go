package goai

import (
	"sort"
	"sync"
)

type ClassifierApi string

type ClassifierProvider string

const (
	ClassifierApiTypeSafeSystemOne        ClassifierApi      = "typesafe-system-one"
	ClassifierApiCloudflareWorkersAI      ClassifierApi      = "cloudflare-workers-ai-system-one"
	ClassifierApiLlamaCPP                 ClassifierApi      = "llama-cpp-classify"
	ClassifierProviderTypeSafe            ClassifierProvider = "typesafe"
	ClassifierProviderOpenRouter          ClassifierProvider = "openrouter"
	ClassifierProviderCloudflareWorkersAI ClassifierProvider = "cloudflare-workers-ai"
	ClassifierProviderVercelAIGateway     ClassifierProvider = "vercel-ai-gateway"
	ClassifierProviderOpenCode            ClassifierProvider = "opencode"
)

type ClassifierModel struct {
	ID            string             `json:"id"`
	Type          string             `json:"type,omitempty"`
	Name          string             `json:"name"`
	Api           ClassifierApi      `json:"api"`
	Provider      ClassifierProvider `json:"provider"`
	BaseURL       string             `json:"baseUrl,omitempty"`
	Headers       map[string]string  `json:"headers,omitempty"`
	Input         []string           `json:"input,omitempty"`
	Cost          ModelCost          `json:"cost"`
	ContextWindow int                `json:"contextWindow"`
	MaxTokens     int                `json:"maxTokens,omitempty"`
}

var (
	classifierRegistryMu sync.RWMutex
	classifierModels     = map[string]*ClassifierModel{}
)

func ClearClassifierModels() {
	classifierRegistryMu.Lock()
	defer classifierRegistryMu.Unlock()
	for key := range classifierModels {
		delete(classifierModels, key)
	}
}

func RegisterClassifierModel(m *ClassifierModel) {
	if m == nil || m.Provider == "" || m.ID == "" {
		return
	}
	classifierRegistryMu.Lock()
	defer classifierRegistryMu.Unlock()
	classifierModels[string(m.Provider)+"/"+m.ID] = cloneClassifierModel(m)
}

func GetClassifierModel(provider ClassifierProvider, id string) *ClassifierModel {
	classifierRegistryMu.RLock()
	defer classifierRegistryMu.RUnlock()
	return cloneClassifierModel(classifierModels[string(provider)+"/"+id])
}

func ListClassifierModels(provider ClassifierProvider) []*ClassifierModel {
	classifierRegistryMu.RLock()
	defer classifierRegistryMu.RUnlock()
	out := []*ClassifierModel{}
	for _, model := range classifierModels {
		if provider == "" || model.Provider == provider {
			out = append(out, cloneClassifierModel(model))
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

func ListClassifierProviders() []ClassifierProvider {
	classifierRegistryMu.RLock()
	defer classifierRegistryMu.RUnlock()
	seen := map[ClassifierProvider]bool{}
	for _, model := range classifierModels {
		seen[model.Provider] = true
	}
	out := make([]ClassifierProvider, 0, len(seen))
	for provider := range seen {
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func cloneClassifierModel(model *ClassifierModel) *ClassifierModel {
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
