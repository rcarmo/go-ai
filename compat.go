// OpenAI Completions compatibility flags for OpenAI-compatible APIs.
package goai

import (
	"net/url"
	"strings"
)

// ChatTemplateKwargValue describes a value for chat_template_kwargs.
// Literal values are used as-is. Variable values resolve to pi-controlled
// thinking state when the provider uses thinkingFormat "chat-template".
type ChatTemplateKwargValue struct {
	Value       interface{} `json:"value,omitempty"`
	Var         string      `json:"$var,omitempty"`
	OmitWhenOff bool        `json:"omitWhenOff,omitempty"`
}

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

	// Whether streamed chunks include finish_reason. When false, a stream that
	// ends without a finish_reason is inferred as stop/toolUse instead of error.
	SupportsFinishReason *bool `json:"supportsFinishReason,omitempty"`

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
	// "zai" = enable_thinking, "qwen" = enable_thinking, "string-thinking" = thinking:<string>
	ThinkingFormat string `json:"thinkingFormat,omitempty"`

	// Kwargs to send as chat_template_kwargs when ThinkingFormat is "chat-template".
	ChatTemplateKwargs map[string]ChatTemplateKwargValue `json:"chatTemplateKwargs,omitempty"`

	// Arguments to send as chat_template_args when ThinkingFormat is "baseten".
	ChatTemplateArgs map[string]ChatTemplateKwargValue `json:"chatTemplateArgs,omitempty"`

	// Whether to emit top-level thinking_token_budget for vLLM-compatible reasoning endpoints.
	SupportsThinkingTokenBudget *bool `json:"supportsThinkingTokenBudget,omitempty"`

	// OpenRouter-specific routing preferences.
	OpenRouterRouting map[string]interface{} `json:"openRouterRouting,omitempty"`

	// Vercel AI Gateway routing preferences.
	VercelGatewayRouting map[string]interface{} `json:"vercelGatewayRouting,omitempty"`

	// Whether z.ai supports top-level `tool_stream: true` for streaming tool deltas.
	ZaiToolStream *bool `json:"zaiToolStream,omitempty"`

	// Whether the provider supports `strict` in tool definitions.
	SupportsStrictMode *bool `json:"supportsStrictMode,omitempty"`

	// Whether the provider supports OpenAI grammar custom tools.
	SupportsOpenAIGrammarTools *bool `json:"supportsOpenAIGrammarTools,omitempty"`

	// Cache control convention: "anthropic" applies cache_control markers.
	CacheControlFormat string `json:"cacheControlFormat,omitempty"`

	// Whether to send session affinity headers for prompt caching.
	SendSessionAffinityHeaders *bool `json:"sendSessionAffinityHeaders,omitempty"`

	// Provider-specific deferred tool serialization mode. "kimi" replays newly-added
	// deferred tools in a post-tool-result system message and omits them from the
	// active top-level tools list until introduced.
	DeferredToolsMode string `json:"deferredToolsMode,omitempty"`

	// Whether the provider supports long prompt cache retention ("24h"). Default: true.
	SupportsLongCacheRetention *bool `json:"supportsLongCacheRetention,omitempty"`

	// Whether the provider supports the temperature parameter. Default: true.
	// Claude Opus 4.7+ rejects non-default temperatures.
	SupportsTemperature *bool `json:"supportsTemperature,omitempty"`
}

// OpenAIResponsesCompat holds compatibility overrides for OpenAI Responses APIs.
type OpenAIResponsesCompat struct {
	// Whether to send the OpenAI session_id cache-affinity header. Default: true.
	SendSessionIdHeader *bool `json:"sendSessionIdHeader,omitempty"`

	// Whether the provider supports long prompt cache retention ("24h"). Default: true.
	SupportsLongCacheRetention *bool `json:"supportsLongCacheRetention,omitempty"`

	// Whether the provider supports message-anchored additional_tools input items.
	SupportsAdditionalTools *bool `json:"supportsAdditionalTools,omitempty"`

	// Whether the provider supports client-side deferred tool loading through tool_search_call/output.
	SupportsToolSearch *bool `json:"supportsToolSearch,omitempty"`

	// Whether the provider supports OpenAI grammar custom tools.
	SupportsOpenAIGrammarTools *bool `json:"supportsOpenAIGrammarTools,omitempty"`

	// Whether the provider supports strict JSON-schema function tools.
	SupportsStrictMode *bool `json:"supportsStrictMode,omitempty"`

	// Provider-specific session affinity header format.
	SessionAffinityFormat string `json:"sessionAffinityFormat,omitempty"`

	// Whether the provider supports explicit prompt cache mode options.
	SupportsExplicitPromptCacheMode *bool `json:"supportsExplicitPromptCacheMode,omitempty"`
}

// AnthropicMessagesCompat holds compatibility overrides for Anthropic-compatible APIs.
type AnthropicMessagesCompat struct {
	// Whether the provider supports deferred tool definitions via tool_reference blocks.
	SupportsToolReferences *bool `json:"supportsToolReferences,omitempty"`

	// Whether the provider accepts per-tool eager_input_streaming.
	// When false, the provider sends the legacy fine-grained-tool-streaming beta header.
	// Default: true.
	SupportsEagerToolInputStreaming *bool `json:"supportsEagerToolInputStreaming,omitempty"`

	// Whether the provider supports Anthropic long cache retention (cache_control.ttl: "1h").
	// Default: true.
	SupportsLongCacheRetention *bool `json:"supportsLongCacheRetention,omitempty"`

	// Whether empty Anthropic thinking signatures should be replayed as `signature: ""` instead of text fallbacks.
	AllowEmptySignature *bool `json:"allowEmptySignature,omitempty"`

	// Whether the provider supports Anthropic strict tool schemas. Default: false.
	SupportsStrictTools *bool `json:"supportsStrictTools,omitempty"`

	// Whether the provider supports cache_control markers on tool definitions. Default: true.
	SupportsCacheControlOnTools *bool `json:"supportsCacheControlOnTools,omitempty"`

	// Whether the provider should emit session-affinity headers when caching is enabled.
	SendSessionAffinityHeaders *bool `json:"sendSessionAffinityHeaders,omitempty"`

	// Whether the provider supports the temperature parameter. Default: true.
	SupportsTemperature *bool `json:"supportsTemperature,omitempty"`

	// Whether the model uses adaptive thinking (type: "adaptive") instead of budget-based thinking.
	// Models like Opus 4.7+, Fable 5 use this mode.
	ForceAdaptiveThinking *bool `json:"forceAdaptiveThinking,omitempty"`
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
	if o.SupportsFinishReason != nil {
		c.SupportsFinishReason = o.SupportsFinishReason
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
	if len(o.ChatTemplateKwargs) > 0 {
		c.ChatTemplateKwargs = o.ChatTemplateKwargs
	}
	if len(o.ChatTemplateArgs) > 0 {
		c.ChatTemplateArgs = o.ChatTemplateArgs
	}
	if o.SupportsThinkingTokenBudget != nil {
		c.SupportsThinkingTokenBudget = o.SupportsThinkingTokenBudget
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
	if o.SupportsOpenAIGrammarTools != nil {
		c.SupportsOpenAIGrammarTools = o.SupportsOpenAIGrammarTools
	}
	if o.CacheControlFormat != "" {
		c.CacheControlFormat = o.CacheControlFormat
	}
	if o.SendSessionAffinityHeaders != nil {
		c.SendSessionAffinityHeaders = o.SendSessionAffinityHeaders
	}
	if o.DeferredToolsMode != "" {
		c.DeferredToolsMode = o.DeferredToolsMode
	}
	if o.SupportsLongCacheRetention != nil {
		c.SupportsLongCacheRetention = o.SupportsLongCacheRetention
	}
	if o.SupportsTemperature != nil {
		c.SupportsTemperature = o.SupportsTemperature
	}
	return c
}

func detectCompat(provider Provider, modelID string, baseURL string) OpenAICompletionsCompat {
	c := OpenAICompletionsCompat{}
	baseURLLower := strings.ToLower(baseURL)

	isOpenRouter := provider == ProviderOpenRouter || contains(baseURLLower, "openrouter.ai")
	isOllama := isLocalOllamaURL(baseURL)
	isZAI := provider == ProviderZAI || provider == ProviderZAICodingCN || contains(baseURLLower, "api.z.ai") || contains(baseURLLower, "open.bigmodel.cn")
	isTogether := provider == ProviderTogether || contains(baseURLLower, "api.together.ai") || contains(baseURLLower, "api.together.xyz")
	isMoonshot := provider == ProviderMoonshotAI || provider == ProviderMoonshotAICN || contains(baseURLLower, "api.moonshot.")
	isCloudflareWorkersAI := provider == ProviderCloudflareWorkersAI || contains(baseURLLower, "api.cloudflare.com")
	isCloudflareAIGW := provider == ProviderCloudflareAIGateway || contains(baseURLLower, "gateway.ai.cloudflare.com")
	isNvidia := provider == ProviderNvidia || contains(baseURLLower, "integrate.api.nvidia.com")
	isAntLing := provider == ProviderAntLing || contains(baseURLLower, "api.ant-ling.com")
	isBaseten := provider == ProviderBaseten || contains(baseURLLower, "inference.baseten.co")
	isGrok := provider == ProviderXAI || contains(baseURLLower, "api.x.ai")
	isDeepSeek := provider == ProviderDeepSeek || contains(baseURLLower, "deepseek.com")
	isXiaomi := provider == ProviderXiaomi || provider == ProviderXiaomiTokenPlanCN || provider == ProviderXiaomiTokenPlanAMS || provider == ProviderXiaomiTokenPlanSGP || contains(baseURLLower, "xiaomimimo.com")

	isNonStandard := isNvidia || provider == ProviderCerebras || contains(baseURLLower, "cerebras.ai") ||
		isGrok || isTogether || contains(baseURLLower, "chutes.ai") || isDeepSeek || isXiaomi ||
		isZAI || isMoonshot || provider == ProviderOpenCode || contains(baseURLLower, "opencode.ai") ||
		isCloudflareWorkersAI || isCloudflareAIGW || isAntLing || isBaseten || isOllama
	useMaxTokens := contains(baseURLLower, "chutes.ai") || isDeepSeek || isMoonshot || isCloudflareAIGW || isTogether || isNvidia || isAntLing || isBaseten || isOllama

	isOpenRouterDevRole := isOpenRouter && (strings.HasPrefix(modelID, "anthropic/") || strings.HasPrefix(modelID, "openai/"))

	t := true
	f := false

	// supportsStore
	if isNonStandard {
		c.SupportsStore = &f
	} else {
		c.SupportsStore = &t
	}

	// supportsDeveloperRole
	if isOpenRouterDevRole || (!isNonStandard && !isOpenRouter) {
		c.SupportsDeveloperRole = &t
	} else {
		c.SupportsDeveloperRole = &f
	}

	// supportsReasoningEffort
	if isGrok || isZAI || isMoonshot || isTogether || isCloudflareAIGW || isNvidia || isAntLing {
		c.SupportsReasoningEffort = &f
	} else {
		c.SupportsReasoningEffort = &t
	}

	c.SupportsUsageInStreaming = &t
	c.SupportsFinishReason = &t

	// maxTokensField
	if useMaxTokens {
		c.MaxTokensField = "max_tokens"
	} else {
		c.MaxTokensField = "max_completion_tokens"
	}

	// requiresReasoningContentOnAssistantMessages
	if isDeepSeek || isXiaomi {
		c.RequiresReasoningContentOnAssistantMessages = &t
	}

	// thinkingFormat (priority order matches upstream ternary chain)
	switch {
	case isDeepSeek || isXiaomi:
		c.ThinkingFormat = "deepseek"
	case isZAI:
		c.ThinkingFormat = "zai"
	case isTogether:
		c.ThinkingFormat = "together"
	case isBaseten:
		c.ThinkingFormat = "baseten"
	case isAntLing:
		c.ThinkingFormat = "ant-ling"
	case isOpenRouter:
		c.ThinkingFormat = "openrouter"
	default:
		c.ThinkingFormat = "openai"
	}

	// supportsStrictMode
	if isMoonshot || isTogether || isCloudflareAIGW || isNvidia {
		c.SupportsStrictMode = &f
	} else {
		c.SupportsStrictMode = &t
	}

	// supportsLongCacheRetention
	if isTogether || isCloudflareWorkersAI || isCloudflareAIGW || isNvidia || isAntLing || isBaseten {
		c.SupportsLongCacheRetention = &f
	} else {
		c.SupportsLongCacheRetention = &t
	}

	// cacheControlFormat
	if isOpenRouter && strings.HasPrefix(modelID, "anthropic/") {
		c.CacheControlFormat = "anthropic"
	}
	if isBaseten {
		c.ChatTemplateArgs = map[string]ChatTemplateKwargValue{"enable_thinking": {Var: "thinking.enabled"}}
	}

	// Ollama-specific overrides
	if isOllama {
		c.RequiresToolResultName = &t
		c.SupportsStrictMode = &f
	}

	return c
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func isLocalOllamaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return u.Port() == "11434" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}
