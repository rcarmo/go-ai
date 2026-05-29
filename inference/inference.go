package inference

import (
	"context"

	goai "github.com/rcarmo/go-ai"
)

type Api = goai.Api
type Provider = goai.Provider
type Model = goai.Model
type Context = goai.Context
type Message = goai.Message
type ContentBlock = goai.ContentBlock
type Tool = goai.Tool
type Usage = goai.Usage
type StreamOptions = goai.StreamOptions
type Event = goai.Event
type ApiProvider = goai.ApiProvider
type ProviderStream = goai.ProviderStream

const (
	ApiOpenAICompletions     = goai.ApiOpenAICompletions
	ApiOpenAIResponses       = goai.ApiOpenAIResponses
	ApiAzureOpenAIResponses  = goai.ApiAzureOpenAIResponses
	ApiOpenAICodexResponses  = goai.ApiOpenAICodexResponses
	ApiAnthropicMessages     = goai.ApiAnthropicMessages
	ApiBedrockConverseStream = goai.ApiBedrockConverseStream
	ApiGoogleGenerativeAI    = goai.ApiGoogleGenerativeAI
	ApiGoogleGeminiCLI       = goai.ApiGoogleGeminiCLI
	ApiGoogleVertex          = goai.ApiGoogleVertex
	ApiMistralConversations  = goai.ApiMistralConversations
)

func RegisterApi(p *ApiProvider)          { goai.RegisterApi(p) }
func UnregisterApi(api Api)               { goai.UnregisterApi(api) }
func GetApiProvider(api Api) *ApiProvider { return goai.GetApiProvider(api) }

func RegisterModel(m *Model)                       { goai.RegisterModel(m) }
func RegisterBuiltinModels()                       { goai.RegisterBuiltinModels() }
func GetModel(provider Provider, id string) *Model { return goai.GetModel(provider, id) }
func ListModels(provider Provider) []*Model        { return goai.ListModels(provider) }
func ListProviders() []Provider                    { return goai.ListProviders() }
func ClearModels()                                 { goai.ClearModels() }
func ClearApiProviders()                           { goai.ClearApiProviders() }

func Stream(ctx context.Context, model *Model, convCtx *Context, opts *StreamOptions) <-chan Event {
	return goai.Stream(ctx, model, convCtx, opts)
}

func Complete(ctx context.Context, model *Model, convCtx *Context, opts *StreamOptions) (*Message, error) {
	return goai.Complete(ctx, model, convCtx, opts)
}
