package images

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type ImagesApi string

type ImagesProvider string

const (
	ImagesApiOpenRouter      ImagesApi      = "openrouter-images"
	ImagesProviderOpenRouter ImagesProvider = "openrouter"
)

type ImageInput struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type ImagesContext struct {
	Input []ImageInput `json:"input"`
}

type ImageOutput struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type ImagesModel struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Api      ImagesApi         `json:"api"`
	Provider ImagesProvider    `json:"provider"`
	BaseURL  string            `json:"baseUrl,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Input    []string          `json:"input,omitempty"`
	Output   []string          `json:"output,omitempty"`
	Cost     goai.ModelCost    `json:"cost"`
}

type AssistantImages struct {
	Api          ImagesApi       `json:"api"`
	Provider     ImagesProvider  `json:"provider"`
	Model        string          `json:"model"`
	Output       []ImageOutput   `json:"output"`
	StopReason   goai.StopReason `json:"stopReason"`
	Timestamp    int64           `json:"timestamp"`
	ResponseID   string          `json:"responseId,omitempty"`
	Usage        *goai.Usage     `json:"usage,omitempty"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
}

type ImagesResponseMetadata struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
}

type ImagesPayloadHook func(payload map[string]any, model *ImagesModel) (map[string]any, error)
type ImagesResponseHook func(response ImagesResponseMetadata, model *ImagesModel) error

type ImagesOptions struct {
	APIKey  string
	Headers map[string]string
	// SuppressHeaders mirrors upstream ProviderHeaders null values: each name
	// removes a provider/model/default header after normal header merging.
	SuppressHeaders []string
	// Signal mirrors upstream's AbortSignal option. Deprecated in Go callers:
	// prefer Context. Signal is honored only when Context is nil.
	Signal  context.Context
	Context context.Context
	Timeout time.Duration
	// TimeoutMs mirrors upstream's timeoutMs option for JSON/JS parity. Prefer
	// Timeout in idiomatic Go code.
	TimeoutMs       int
	MaxRetries      int
	MaxRetryDelayMs int
	Metadata        map[string]any
	Env             goai.ProviderEnv
	HTTPClient      *http.Client
	OnPayload       ImagesPayloadHook
	OnResponse      ImagesResponseHook
}

type ImagesFunction func(model *ImagesModel, ctx ImagesContext, options *ImagesOptions) (*AssistantImages, error)

type ImagesApiProvider struct {
	Api            ImagesApi
	GenerateImages ImagesFunction
}

var (
	imagesRegistryMu   sync.RWMutex
	imagesApiProviders = map[ImagesApi]*ImagesApiProvider{}
	imageModels        = map[string]*ImagesModel{}
)

func RegisterImagesApiProvider(p *ImagesApiProvider) {
	if p == nil || p.Api == "" || p.GenerateImages == nil {
		return
	}
	imagesRegistryMu.Lock()
	defer imagesRegistryMu.Unlock()
	imagesApiProviders[p.Api] = p
}

func GetImagesApiProvider(api ImagesApi) *ImagesApiProvider {
	imagesRegistryMu.RLock()
	defer imagesRegistryMu.RUnlock()
	return imagesApiProviders[api]
}

// UnregisterImagesApiProvider removes an image API provider by API name.
func UnregisterImagesApiProvider(api ImagesApi) {
	imagesRegistryMu.Lock()
	defer imagesRegistryMu.Unlock()
	delete(imagesApiProviders, api)
}

// ClearImagesApiProviders removes all registered image API providers.
func ClearImagesApiProviders() {
	imagesRegistryMu.Lock()
	defer imagesRegistryMu.Unlock()
	for k := range imagesApiProviders {
		delete(imagesApiProviders, k)
	}
}

// ClearImageModels removes all registered image models.
func ClearImageModels() {
	imagesRegistryMu.Lock()
	defer imagesRegistryMu.Unlock()
	for k := range imageModels {
		delete(imageModels, k)
	}
}

func RegisterImageModel(m *ImagesModel) {
	if m == nil || m.Provider == "" || m.ID == "" {
		return
	}
	imagesRegistryMu.Lock()
	defer imagesRegistryMu.Unlock()
	imageModels[string(m.Provider)+"/"+m.ID] = m
}

func GetImageModel(provider ImagesProvider, id string) *ImagesModel {
	imagesRegistryMu.RLock()
	defer imagesRegistryMu.RUnlock()
	return imageModels[string(provider)+"/"+id]
}

func ListImageModels(provider ImagesProvider) []*ImagesModel {
	imagesRegistryMu.RLock()
	defer imagesRegistryMu.RUnlock()
	out := []*ImagesModel{}
	for _, m := range imageModels {
		if provider == "" || m.Provider == provider {
			out = append(out, m)
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

func ListImageProviders() []ImagesProvider {
	imagesRegistryMu.RLock()
	defer imagesRegistryMu.RUnlock()
	seen := map[ImagesProvider]bool{}
	for _, m := range imageModels {
		seen[m.Provider] = true
	}
	out := make([]ImagesProvider, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// GenerateImages invokes the registered image API provider for model.
//
// Provider/runtime failures mirror upstream pi-ai: they are returned as an
// AssistantImages value with StopReason "error" or "aborted" and ErrorMessage
// populated. The Go error return is reserved for future non-provider failures;
// callers should inspect StopReason just as JS callers inspect the resolved
// AssistantImages object.
func GenerateImages(model *ImagesModel, ctx ImagesContext, opts *ImagesOptions) (*AssistantImages, error) {
	if model == nil {
		return &AssistantImages{StopReason: goai.StopReasonError, ErrorMessage: "nil model"}, nil
	}
	p := GetImagesApiProvider(model.Api)
	if p == nil {
		return &AssistantImages{Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonError, ErrorMessage: "no image provider registered"}, nil
	}
	return p.GenerateImages(model, ctx, opts)
}
