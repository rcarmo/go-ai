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

	// Mapping from thinking levels to provider-specific reasoning_effort values.
	ReasoningEffortMap map[ThinkingLevel]string `json:"reasoningEffortMap,omitempty"`

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
// This mirrors pi-ai's URL-based auto-detection for known providers.
func DetectCompat(baseURL string) OpenAICompletionsCompat {
	c := OpenAICompletionsCompat{}

	isOpenAI := contains(baseURL, "api.openai.com")
	isGroq := contains(baseURL, "groq.com")
	isCerebras := contains(baseURL, "cerebras.ai")
	isXAI := contains(baseURL, "x.ai") || contains(baseURL, "xai.com")
	isOpenRouter := contains(baseURL, "openrouter.ai")
	isOllama := contains(baseURL, "localhost:11434") || contains(baseURL, ":11434")
	isZAI := contains(baseURL, "z.ai") || contains(baseURL, "zai.com")
	isVercel := contains(baseURL, "gateway.vercel.ai") || contains(baseURL, "sdk.vercel.ai")
	isQwen := contains(baseURL, "dashscope.aliyuncs.com")
	isDeepSeek := contains(baseURL, "deepseek.com")
	isCloudflare := contains(baseURL, "api.cloudflare.com")

	t := true
	f := false

	if isOpenAI {
		c.SupportsDeveloperRole = &t
		c.SupportsReasoningEffort = &t
		c.MaxTokensField = "max_completion_tokens"
		c.SupportsStrictMode = &t
	} else {
		c.SupportsDeveloperRole = &f
		c.MaxTokensField = "max_tokens"
	}

	if isGroq || isCerebras {
		c.SupportsReasoningEffort = &f
		c.SupportsUsageInStreaming = &t
	}

	if isXAI {
		c.SupportsReasoningEffort = &t
	}

	if isOpenRouter {
		c.ThinkingFormat = "openrouter"
		c.SupportsUsageInStreaming = &t
	}

	if isOllama {
		c.SupportsReasoningEffort = &f
		c.SupportsUsageInStreaming = &t
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

	if isCloudflare {
		c.SupportsReasoningEffort = &f
		c.SupportsUsageInStreaming = &t
	}

	return c
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
