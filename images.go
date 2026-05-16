package goai

import "context"

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
	Output   []string          `json:"output,omitempty"`
	Cost     ModelCost         `json:"cost"`
}

type AssistantImages struct {
	Api          ImagesApi      `json:"api"`
	Provider     ImagesProvider `json:"provider"`
	Model        string         `json:"model"`
	Output       []ImageOutput  `json:"output"`
	StopReason   StopReason     `json:"stopReason"`
	Timestamp    int64          `json:"timestamp"`
	ResponseID   string         `json:"responseId,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
}

type ImagesOptions struct {
	APIKey  string
	Headers map[string]string
	Signal  context.Context
}

type ImagesFunction func(model *ImagesModel, ctx ImagesContext, options *ImagesOptions) (*AssistantImages, error)

type ImagesApiProvider struct {
	Api            ImagesApi
	GenerateImages ImagesFunction
}

var imagesApiProviders = map[ImagesApi]*ImagesApiProvider{}

func RegisterImagesApiProvider(p *ImagesApiProvider) {
	if p == nil || p.Api == "" || p.GenerateImages == nil {
		return
	}
	imagesApiProviders[p.Api] = p
}

func GetImagesApiProvider(api ImagesApi) *ImagesApiProvider {
	return imagesApiProviders[api]
}

func GenerateImages(model *ImagesModel, ctx ImagesContext, opts *ImagesOptions) (*AssistantImages, error) {
	if model == nil {
		return &AssistantImages{StopReason: StopReasonError, ErrorMessage: "nil model"}, nil
	}
	p := GetImagesApiProvider(model.Api)
	if p == nil {
		return &AssistantImages{Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: StopReasonError, ErrorMessage: "no image provider registered"}, nil
	}
	return p.GenerateImages(model, ctx, opts)
}
