// Package goai — core types for the unified LLM API.
//
// These types mirror @mariozechner/pi-ai's type system exactly,
// enabling serialization-compatible context hand-off between the
// TypeScript and Go implementations.
package goai

import "encoding/json"

// --- Enums / string types ---

// Api identifies a provider wire protocol.
type Api string

const (
	ApiOpenAICompletions     Api = "openai-completions"
	ApiOpenAIResponses       Api = "openai-responses"
	ApiAzureOpenAIResponses  Api = "azure-openai-responses"
	ApiOpenAICodexResponses  Api = "openai-codex-responses"
	ApiAnthropicMessages     Api = "anthropic-messages"
	ApiBedrockConverseStream Api = "bedrock-converse-stream"
	ApiGoogleGenerativeAI    Api = "google-generative-ai"
	ApiGoogleGeminiCLI       Api = "google-gemini-cli"
	ApiGoogleVertex          Api = "google-vertex"
	ApiMistralConversations  Api = "mistral-conversations"
)

// Provider identifies a model hosting service.
type Provider string

const (
	ProviderOpenAI              Provider = "openai"
	ProviderAnthropic           Provider = "anthropic"
	ProviderGoogle              Provider = "google"
	ProviderGoogleGeminiCLI     Provider = "google-gemini-cli"
	ProviderGoogleAntigravity   Provider = "google-antigravity"
	ProviderGoogleVertex        Provider = "google-vertex"
	ProviderAzureOpenAI         Provider = "azure-openai-responses"
	ProviderOpenAICodex         Provider = "openai-codex"
	ProviderGitHubCopilot       Provider = "github-copilot"
	ProviderAmazonBedrock       Provider = "amazon-bedrock"
	ProviderMistral             Provider = "mistral"
	ProviderXAI                 Provider = "xai"
	ProviderGroq                Provider = "groq"
	ProviderCerebras            Provider = "cerebras"
	ProviderOpenRouter          Provider = "openrouter"
	ProviderVercelAIGateway     Provider = "vercel-ai-gateway"
	ProviderZAI                 Provider = "zai"
	ProviderMiniMax             Provider = "minimax"
	ProviderMiniMaxCN           Provider = "minimax-cn"
	ProviderHuggingFace         Provider = "huggingface"
	ProviderFireworks           Provider = "fireworks"
	ProviderOpenCode            Provider = "opencode"
	ProviderOpenCodeGo          Provider = "opencode-go"
	ProviderKimiCoding          Provider = "kimi-coding"
	ProviderDeepSeek            Provider = "deepseek"
	ProviderCloudflareWorkersAI Provider = "cloudflare-workers-ai"
	ProviderCloudflareAIGateway Provider = "cloudflare-ai-gateway"
	ProviderMoonshotAI          Provider = "moonshotai"
	ProviderMoonshotAICN        Provider = "moonshotai-cn"
)

// ThinkingLevel controls the reasoning depth.
type ThinkingLevel string

const (
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
)

// Role identifies the sender of a message.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopReasonStop    StopReason = "stop"
	StopReasonLength  StopReason = "length"
	StopReasonToolUse StopReason = "toolUse"
	StopReasonError   StopReason = "error"
	StopReasonAborted StopReason = "aborted"
)

// CacheRetention controls prompt cache lifetime preference.
type CacheRetention string

const (
	CacheRetentionNone  CacheRetention = "none"
	CacheRetentionShort CacheRetention = "short"
	CacheRetentionLong  CacheRetention = "long"
)

// Transport selects the wire transport.
type Transport string

const (
	TransportSSE       Transport = "sse"
	TransportWebSocket Transport = "websocket"
	TransportAuto      Transport = "auto"
)

// --- Content types ---

// TextContent represents text in a message.
type TextContent struct {
	Type          string `json:"type"` // always "text"
	Text          string `json:"text"`
	TextSignature string `json:"textSignature,omitempty"`
}

// ThinkingContent represents model reasoning.
type ThinkingContent struct {
	Type              string `json:"type"` // always "thinking"
	Thinking          string `json:"thinking"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`
}

// ImageContent represents an inline image.
type ImageContent struct {
	Type     string `json:"type"` // always "image"
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// ToolCall represents a function call request from the model.
type ToolCall struct {
	Type             string                 `json:"type"` // always "toolCall"
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Arguments        map[string]interface{} `json:"arguments"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
}

// ContentBlock is any content element in a message.
// Use the Type field to discriminate.
type ContentBlock struct {
	// Discriminator: "text", "thinking", "image", "toolCall"
	Type string `json:"type"`

	// Text fields (type == "text")
	Text          string `json:"text,omitempty"`
	TextSignature string `json:"textSignature,omitempty"`

	// Thinking fields (type == "thinking")
	Thinking          string `json:"thinking,omitempty"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`

	// Image fields (type == "image")
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`

	// ToolCall fields (type == "toolCall")
	ID               string                 `json:"id,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Arguments        map[string]interface{} `json:"arguments,omitempty"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
}

// --- Usage ---

// CostBreakdown holds per-category costs in USD.
type CostBreakdown struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Usage tracks token counts and costs for a single request.
type Usage struct {
	Input       int           `json:"input"`
	Output      int           `json:"output"`
	CacheRead   int           `json:"cacheRead"`
	CacheWrite  int           `json:"cacheWrite"`
	TotalTokens int           `json:"totalTokens"`
	Cost        CostBreakdown `json:"cost"`
}

// --- Messages ---

// Message is a single conversation turn.
type Message struct {
	Role      Role           `json:"role"`
	Content   []ContentBlock `json:"content"`
	Timestamp int64          `json:"timestamp"`

	// Assistant-only fields
	Api           Api        `json:"api,omitempty"`
	Provider      Provider   `json:"provider,omitempty"`
	Model         string     `json:"model,omitempty"`
	ResponseID    string     `json:"responseId,omitempty"`
	ResponseModel string     `json:"responseModel,omitempty"`
	Usage         *Usage     `json:"usage,omitempty"`
	StopReason    StopReason `json:"stopReason,omitempty"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`

	// ToolResult-only fields
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	IsError    bool   `json:"isError,omitempty"`
	Details    any    `json:"details,omitempty"`
}

// UserMessage creates a simple text user message.
func UserMessage(text string) Message {
	return Message{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: "text", Text: text}},
	}
}

// --- Tool definition ---

// Tool defines a function the model can call.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// --- Context ---

// Context holds the conversation state passed to stream/complete.
type Context struct {
	SystemPrompt string    `json:"systemPrompt,omitempty"`
	Messages     []Message `json:"messages"`
	Tools        []Tool    `json:"tools,omitempty"`
}

// --- Model ---

// ModelCost holds per-million-token costs.
type ModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// Model identifies a specific LLM endpoint.
type Model struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Api           Api       `json:"api"`
	Provider      Provider  `json:"provider"`
	BaseURL       string    `json:"baseUrl"`
	Reasoning     bool      `json:"reasoning"`
	Input         []string  `json:"input"` // "text", "image"
	Cost          ModelCost `json:"cost"`
	ContextWindow int       `json:"contextWindow"`
	MaxTokens     int       `json:"maxTokens"`

	// Optional overrides
	Headers           map[string]string        `json:"headers,omitempty"`
	APIKey            string                   `json:"-"` // never serialized
	CompletionsCompat *OpenAICompletionsCompat `json:"completionsCompat,omitempty"`
	ResponsesCompat   *OpenAIResponsesCompat   `json:"responsesCompat,omitempty"`
	AnthropicCompat   *AnthropicMessagesCompat `json:"anthropicCompat,omitempty"`
}

// --- Stream options ---

// ThinkingBudgets maps thinking levels to token budgets.
type ThinkingBudgets struct {
	Minimal *int `json:"minimal,omitempty"`
	Low     *int `json:"low,omitempty"`
	Medium  *int `json:"medium,omitempty"`
	High    *int `json:"high,omitempty"`
}

// StreamOptions controls a single stream/complete request.
type StreamOptions struct {
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxTokens       *int              `json:"maxTokens,omitempty"`
	APIKey          string            `json:"-"`
	Transport       Transport         `json:"transport,omitempty"`
	CacheRetention  CacheRetention    `json:"cacheRetention,omitempty"`
	SessionID       string            `json:"sessionId,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	MaxRetryDelayMs *int              `json:"maxRetryDelayMs,omitempty"`
	RetryConfig     *RetryConfig      `json:"-"`
	Metadata        map[string]any    `json:"metadata,omitempty"`

	// SDK-level timeout and retry (for providers that use SDK clients).
	// These are passed through to the underlying SDK client when applicable.
	TimeoutMs  *int `json:"timeoutMs,omitempty"`
	MaxRetries *int `json:"maxRetries,omitempty"`

	// Simple mode
	Reasoning       *ThinkingLevel   `json:"reasoning,omitempty"`
	ThinkingBudgets *ThinkingBudgets `json:"thinkingBudgets,omitempty"`

	// Hooks for request/response interception

	// OnPayload is called with the serialized request body before sending.
	// Return a modified payload to replace it, or nil to keep the original.
	OnPayload func(payload interface{}, model *Model) (interface{}, error) `json:"-"`

	// OnResponse is called after receiving the HTTP response headers.
	OnResponse func(status int, headers map[string]string, model *Model) `json:"-"`
}
