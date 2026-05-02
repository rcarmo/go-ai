// OpenAI Completions compatibility flags for OpenAI-compatible APIs.
package goai

import "strings"

// OpenAICompletionsCompat holds compatibility overrides for OpenAI-compatible APIs.
// These control wire-format differences across Ollama, Groq, xAI, OpenRouter,
// vLLM, LM Studio, z.ai, and other providers.
type OpenAICompletionsCompat struct {
	// Whether the provider supports the `store` field. Default: auto-detected from URL.
	SupportsStore *bool `json:"supportsStore,omitempty"`

	// Whether the provider supports the `developer` role (vs `system`).
	SupportsDeveloperRole *bool `json:"supportsDeveloperRole,omitempty"`

	// Whether the provider supports `reasoning_effort`.
	SupportsReasoningEffort *bool `json:"supportsReasoningEffort,omitempty"`

	// Whether the provider supports `stream_options: { include_usage: true }`.
	SupportsUsageInStreaming *bool `json:"supportsUsageInStreaming,omitempty"`

	// Which field to use for max tokens: "max_completion_tokens" or "max_tokens".
	MaxTokensField string `json:"maxTokensField,omitempty"`

	// Whether tool results require the `name` field.
	RequiresToolResultName *bool `json:"requiresToolResultName,omitempty"`

	// Whether a user message after tool results requires an assistant message in between.
	RequiresAssistantAfterToolResult *bool `json:"requiresAssistantAfterToolResult,omitempty"`

	// Whether thinking blocks must be converted to text with <thinking> delimiters.
	RequiresThinkingAsText *bool `json:"requiresThinkingAsText,omitempty"`

	// Whether all replayed assistant messages must include an empty reasoning_content field when reasoning is enabled.
	RequiresReasoningContentOnAssistantMessages *bool `json:"requiresReasoningContentOnAssistantMessages,omitempty"`

	// Format for reasoning/thinking parameter.
	// "openai" = reasoning_effort, "openrouter" = reasoning:{effort}, "deepseek" = thinking:{type} + reasoning_effort,
	// "zai" = enable_thinking, "qwen" = enable_thinking
	ThinkingFormat string `json:"thinkingFormat,omitempty"`

	// OpenRouter-specific routing preferences.
	OpenRouterRouting map[string]interface{} `json:"openRouterRouting,omitempty"`

	// Vercel AI Gateway routing preferences.
	VercelGatewayRouting map[string]interface{} `json:"vercelGatewayRouting,omitempty"`

	// Whether z.ai supports top-level `tool_stream: true` for streaming tool deltas.
	ZaiToolStream *bool `json:"zaiToolStream,omitempty"`

	// Whether the provider supports `strict` in tool definitions.
	SupportsStrictMode *bool `json:"supportsStrictMode,omitempty"`

	// Cache control convention: "anthropic" applies cache_control markers.
	CacheControlFormat string `json:"cacheControlFormat,omitempty"`

	// Whether to send session affinity headers for prompt caching.
	SendSessionAffinityHeaders *bool `json:"sendSessionAffinityHeaders,omitempty"`

	// Whether the provider supports long prompt cache retention ("24h"). Default: true.
	SupportsLongCacheRetention *bool `json:"supportsLongCacheRetention,omitempty"`
}

// OpenAIResponsesCompat holds compatibility overrides for OpenAI Responses APIs.
type OpenAIResponsesCompat struct {
	// Whether to send the OpenAI session_id cache-affinity header. Default: true.
	SendSessionIdHeader *bool `json:"sendSessionIdHeader,omitempty"`

	// Whether the provider supports long prompt cache retention ("24h"). Default: true.
	SupportsLongCacheRetention *bool `json:"supportsLongCacheRetention,omitempty"`
}

// AnthropicMessagesCompat holds compatibility overrides for Anthropic-compatible APIs.
type AnthropicMessagesCompat struct {
	// Whether the provider accepts per-tool eager_input_streaming.
	// When false, the provider sends the legacy fine-grained-tool-streaming beta header.
	// Default: true.
	SupportsEagerToolInputStreaming *bool `json:"supportsEagerToolInputStreaming,omitempty"`

	// Whether the provider supports Anthropic long cache retention (cache_control.ttl: "1h").
	// Default: true.
	SupportsLongCacheRetention *bool `json:"supportsLongCacheRetention,omitempty"`
}

// DetectCompat auto-detects compatibility flags from a base URL.
// Prefer DetectCompatForModel when a Model is available, since recent pi-ai
// releases make provider ID take precedence over URL heuristics.
func DetectCompat(baseURL string) OpenAICompletionsCompat {
	return detectCompat("", "", baseURL)
}

// DetectCompatForModel auto-detects and merges OpenAI-compatible API flags for a model.
// This mirrors pi-ai's provider-first detection plus explicit model compat overrides.
func DetectCompatForModel(model *Model) OpenAICompletionsCompat {
	if model == nil {
		return OpenAICompletionsCompat{}
	}
	c := detectCompat(model.Provider, model.ID, model.BaseURL)
	if model.CompletionsCompat == nil {
		return c
	}
	o := model.CompletionsCompat
	if o.SupportsStore != nil {
		c.SupportsStore = o.SupportsStore
	}
	if o.SupportsDeveloperRole != nil {
		c.SupportsDeveloperRole = o.SupportsDeveloperRole
	}
	if o.SupportsReasoningEffort != nil {
		c.SupportsReasoningEffort = o.SupportsReasoningEffort
	}
	if o.SupportsUsageInStreaming != nil {
		c.SupportsUsageInStreaming = o.SupportsUsageInStreaming
	}
	if o.MaxTokensField != "" {
		c.MaxTokensField = o.MaxTokensField
	}
	if o.RequiresToolResultName != nil {
		c.RequiresToolResultName = o.RequiresToolResultName
	}
	if o.RequiresAssistantAfterToolResult != nil {
		c.RequiresAssistantAfterToolResult = o.RequiresAssistantAfterToolResult
	}
	if o.RequiresThinkingAsText != nil {
		c.RequiresThinkingAsText = o.RequiresThinkingAsText
	}
	if o.RequiresReasoningContentOnAssistantMessages != nil {
		c.RequiresReasoningContentOnAssistantMessages = o.RequiresReasoningContentOnAssistantMessages
	}
	if o.ThinkingFormat != "" {
		c.ThinkingFormat = o.ThinkingFormat
	}
	if o.OpenRouterRouting != nil {
		c.OpenRouterRouting = o.OpenRouterRouting
	}
	if o.VercelGatewayRouting != nil {
		c.VercelGatewayRouting = o.VercelGatewayRouting
	}
	if o.ZaiToolStream != nil {
		c.ZaiToolStream = o.ZaiToolStream
	}
	if o.SupportsStrictMode != nil {
		c.SupportsStrictMode = o.SupportsStrictMode
	}
	if o.CacheControlFormat != "" {
		c.CacheControlFormat = o.CacheControlFormat
	}
	if o.SendSessionAffinityHeaders != nil {
		c.SendSessionAffinityHeaders = o.SendSessionAffinityHeaders
	}
	if o.SupportsLongCacheRetention != nil {
		c.SupportsLongCacheRetention = o.SupportsLongCacheRetention
	}
	return c
}

func detectCompat(provider Provider, modelID string, baseURL string) OpenAICompletionsCompat {
	c := OpenAICompletionsCompat{}

	isGroq := provider == ProviderGroq || contains(baseURL, "groq.com")
	isCerebras := provider == ProviderCerebras || contains(baseURL, "cerebras.ai")
	isXAI := provider == ProviderXAI || contains(baseURL, "x.ai") || contains(baseURL, "xai.com")
	isOpenRouter := provider == ProviderOpenRouter || contains(baseURL, "openrouter.ai")
	isOllama := contains(baseURL, "localhost:11434") || contains(baseURL, ":11434")
	isZAI := provider == ProviderZAI || contains(baseURL, "z.ai") || contains(baseURL, "zai.com")
	isVercel := provider == ProviderVercelAIGateway || contains(baseURL, "gateway.vercel.ai") || contains(baseURL, "sdk.vercel.ai")
	isQwen := contains(baseURL, "dashscope.aliyuncs.com")
	isDeepSeek := provider == ProviderDeepSeek || contains(baseURL, "deepseek.com")
	isMoonshot := provider == ProviderMoonshotAI || provider == ProviderMoonshotAICN || contains(baseURL, "api.moonshot.")
	isCloudflareWorkersAI := provider == ProviderCloudflareWorkersAI || contains(baseURL, "api.cloudflare.com")
	isCloudflareAIGW := provider == ProviderCloudflareAIGateway || contains(baseURL, "gateway.ai.cloudflare.com")
	isNonStandard := isCerebras || isXAI || contains(baseURL, "chutes.ai") || isDeepSeek || isZAI || isMoonshot || provider == ProviderOpenCode || contains(baseURL, "opencode.ai") || isCloudflareWorkersAI || isCloudflareAIGW || isOllama
	useMaxTokens := contains(baseURL, "chutes.ai") || isMoonshot || isCloudflareAIGW || isOllama

	t := true
	f := false

	c.SupportsStore = &t
	c.SupportsDeveloperRole = &t
	c.SupportsReasoningEffort = &t
	c.SupportsUsageInStreaming = &t
	c.SupportsStrictMode = &t
	c.SupportsLongCacheRetention = &t
	c.MaxTokensField = "max_completion_tokens"
	if useMaxTokens {
		c.MaxTokensField = "max_tokens"
	}
	if isNonStandard {
		c.SupportsStore = &f
		c.SupportsDeveloperRole = &f
	}

	if isGroq || isCerebras || isMoonshot || isCloudflareAIGW || isCloudflareWorkersAI {
		c.SupportsReasoningEffort = &f
	}
	if isXAI {
		c.SupportsReasoningEffort = &t
	}
	if isOpenRouter {
		c.ThinkingFormat = "openrouter"
	}
	if isOllama {
		c.RequiresToolResultName = &t
		c.SupportsStrictMode = &f
	}
	if isZAI {
		c.ThinkingFormat = "zai"
	}
	if isVercel {
		c.SupportsUsageInStreaming = &t
	}
	if isQwen {
		c.ThinkingFormat = "qwen"
	}
	if isDeepSeek {
		c.ThinkingFormat = "deepseek"
		c.RequiresReasoningContentOnAssistantMessages = &t
	}
	if isMoonshot || isCloudflareAIGW {
		c.SupportsStrictMode = &f
	}
	if isCloudflareWorkersAI || isCloudflareAIGW {
		c.SupportsLongCacheRetention = &f
	}
	if isOpenRouter && strings.HasPrefix(modelID, "anthropic/") {
		c.CacheControlFormat = "anthropic"
	}
	return c
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
