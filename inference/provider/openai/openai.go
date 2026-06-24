// Package openai implements the OpenAI Chat Completions API provider.
//
// This handles both native OpenAI and any OpenAI-compatible API
// (Ollama, vLLM, LM Studio, Groq, Cerebras, xAI, OpenRouter, etc.).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/sse"
)

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiOpenAICompletions,
		Stream:       streamOpenAI,
		StreamSimple: streamOpenAISimple,
	})
}

// streamOpenAISimple wraps streamOpenAI with thinking-level mapping.
func streamOpenAISimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	// Map reasoning level to reasoning_effort if supported
	// For now, pass through to streamOpenAI
	return streamOpenAI(ctx, model, convCtx, opts)
}

// streamOpenAI implements the OpenAI Chat Completions streaming protocol.
func streamOpenAI(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "openai-completions", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		var optHeaders map[string]string
		var suppressHeaders []string
		if opts != nil {
			optHeaders = opts.Headers
			suppressHeaders = opts.SuppressHeaders
		}
		if apiKey == "" && !goai.HasOpenAIAuthHeader(goai.MergeProviderHeaders(model.Headers, optHeaders, suppressHeaders)) {
			ch <- &goai.ErrorEvent{
				Reason: goai.StopReasonError,
				//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
				Err: fmt.Errorf("No API key for provider: %s", model.Provider),
			}
			return
		}

		// Build request body
		body := buildRequestBody(model, convCtx, opts)
		payload, err := goai.InvokeOnPayload(opts, body, model)
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		bodyJSON, err := json.Marshal(payload)
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		baseURL := model.BaseURL
		if goai.IsCloudflareProvider(model.Provider) {
			baseURL = goai.ResolveCloudflareBaseURL(model, goai.ProviderEnvFromOptions(opts))
		}
		url := baseURL + "/chat/completions"
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			if model.Provider == goai.ProviderCloudflareAIGateway {
				req.Header.Set("cf-aig-authorization", "Bearer "+apiKey)
			} else {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
		}
		req.Header.Set("Accept", "text/event-stream")

		// Session affinity headers for prompt caching
		compat := goai.DetectCompatForModel(model)
		if compat.SendSessionAffinityHeaders != nil && *compat.SendSessionAffinityHeaders && opts != nil && opts.SessionID != "" {
			req.Header.Set("session_id", opts.SessionID)
			req.Header.Set("x-client-request-id", opts.SessionID)
			req.Header.Set("x-session-affinity", opts.SessionID)
		}

		// Dynamic Copilot headers
		if model.Provider == goai.ProviderGitHubCopilot {
			for k, v := range goai.BuildCopilotDynamicHeaders(convCtx.Messages) {
				req.Header.Set(k, v)
			}
		}

		// Apply custom headers
		if opts != nil {
			goai.ApplyHeaders(req.Header, opts.Headers)
		}
		goai.ApplyDefaultHeaders(req.Header, model.Headers)
		if opts != nil {
			goai.SuppressHeaders(req.Header, opts.SuppressHeaders)
		}

		retryCfg := goai.RetryConfigFromOptions(opts)
		client := retryCfg.NewHTTPClient()
		goai.GetLogger().Debug("HTTP request", "url", req.URL.String(), "provider", model.Provider, "model", model.ID, "retries", retryCfg.MaxRetries)
		resp, err := goai.DoWithRetry(ctx, client, req, retryCfg)
		if err != nil {
			if ctx.Err() != nil {
				goai.GetLogger().Debug("request aborted", "provider", model.Provider, "model", model.ID)
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()}
			} else {
				goai.GetLogger().Warn("network error", "provider", model.Provider, "model", model.ID, "error", err)
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			}
			return
		}
		defer resp.Body.Close()

		goai.InvokeOnResponse(opts, resp, model)

		if resp.StatusCode != 200 {
			goai.GetLogger().Warn("HTTP error response", "status", resp.StatusCode, "provider", model.Provider, "model", model.ID)
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			ch <- &goai.ErrorEvent{
				Reason: goai.StopReasonError,
				Err:    fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes)),
			}
			return
		}

		processSSEStream(resp.Body, model, ch)
	}()

	return ch
}

// --- Request building ---

type chatRequest struct {
	Model                string                 `json:"model"`
	Messages             []chatMessage          `json:"messages"`
	Stream               bool                   `json:"stream"`
	StreamOptions        *streamOpts            `json:"stream_options,omitempty"`
	PromptCacheKey       string                 `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string                 `json:"prompt_cache_retention,omitempty"`
	Temperature          *float64               `json:"temperature,omitempty"`
	MaxTokens            *int                   `json:"max_tokens,omitempty"`
	MaxCompletionToks    *int                   `json:"max_completion_tokens,omitempty"`
	Tools                []toolDef              `json:"tools,omitempty"`
	ReasoningEffort      string                 `json:"reasoning_effort,omitempty"`
	Reasoning            map[string]interface{} `json:"reasoning,omitempty"`
	Thinking             map[string]interface{} `json:"thinking,omitempty"`
	EnableThinking       *bool                  `json:"enable_thinking,omitempty"`
	ChatTemplateKwargs   map[string]interface{} `json:"chat_template_kwargs,omitempty"`
	ToolStream           *bool                  `json:"tool_stream,omitempty"`
	Store                *bool                  `json:"store,omitempty"`
}

type streamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role             string            `json:"role"`
	Content          interface{}       `json:"content"` // string or []contentPart
	ReasoningDetails []reasoningDetail `json:"reasoning_details,omitempty"`
	ToolCalls        []toolCallPart    `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	Name             string            `json:"name,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type contentPart struct {
	Type         string        `json:"type"`
	Text         string        `json:"text,omitempty"`
	ImageURL     *imageURL     `json:"image_url,omitempty"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type toolDef struct {
	Type         string        `json:"type"`
	Function     toolFunction  `json:"function"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

type toolCallPart struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func buildRequestBody(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) chatRequest {
	// Detect compat flags from provider/base URL plus explicit model overrides.
	compat := goai.DetectCompatForModel(model)

	req := chatRequest{
		Model:  model.ID,
		Stream: true,
	}

	// Stream options — some providers don't support include_usage
	if compat.SupportsUsageInStreaming == nil || *compat.SupportsUsageInStreaming {
		req.StreamOptions = &streamOpts{IncludeUsage: true}
	}

	// Prompt cache/session fields. Match pi-ai: send a cache key when cache
	// retention is enabled, and only request 24h retention when supported.
	cacheRetention := goai.CacheRetentionShort
	if opts != nil {
		cacheRetention = goai.ResolveCacheRetention(opts.CacheRetention, opts.Env)
	}
	if opts != nil && opts.SessionID != "" && cacheRetention != goai.CacheRetentionNone {
		if (strings.Contains(model.BaseURL, "api.openai.com") && cacheRetention != goai.CacheRetentionNone) || (cacheRetention == goai.CacheRetentionLong && (compat.SupportsLongCacheRetention == nil || *compat.SupportsLongCacheRetention)) {
			req.PromptCacheKey = goai.ClampOpenAIPromptCacheKey(opts.SessionID)
		}
	}
	if cacheRetention == goai.CacheRetentionLong && (compat.SupportsLongCacheRetention == nil || *compat.SupportsLongCacheRetention) {
		req.PromptCacheRetention = "24h"
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		// Max tokens field depends on provider
		if compat.MaxTokensField == "max_completion_tokens" {
			req.MaxCompletionToks = opts.MaxTokens
		} else {
			req.MaxTokens = opts.MaxTokens
		}
	}

	// Store field
	if compat.SupportsStore != nil && *compat.SupportsStore {
		t := true
		req.Store = &t
	}

	// Reasoning effort / thinking control. Mirror pi-ai's provider-specific formats.
	reasoningRequested := opts != nil && opts.Reasoning != nil
	effort := ""
	if reasoningRequested {
		if mapped, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning)); ok {
			effort = mapped
		}
	}
	if model.Reasoning {
		switch compat.ThinkingFormat {
		case "zai":
			typeValue := "disabled"
			if reasoningRequested && effort != "" {
				typeValue = "enabled"
			}
			req.Thinking = map[string]interface{}{"type": typeValue}
		case "qwen":
			enabled := reasoningRequested && effort != ""
			req.EnableThinking = &enabled
		case "string-thinking":
			if reasoningRequested && effort != "" {
				req.Thinking = map[string]interface{}{"type": effort}
			} else if off, ok := model.ThinkingLevelMap[goai.ThinkingOff]; ok && off != nil {
				req.Thinking = map[string]interface{}{"type": *off}
			}
		case "qwen-chat-template":
			enabled := reasoningRequested && effort != ""
			req.ChatTemplateKwargs = map[string]interface{}{"enable_thinking": enabled, "preserve_thinking": true}
		case "chat-template":
			if kwargs := buildChatTemplateKwargs(model, opts, compat, effort); len(kwargs) > 0 {
				req.ChatTemplateKwargs = kwargs
			}
		case "deepseek":
			typeValue := "disabled"
			if reasoningRequested && effort != "" {
				typeValue = "enabled"
				if compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort {
					req.ReasoningEffort = effort
				}
			}
			req.Thinking = map[string]interface{}{"type": typeValue}
		case "openrouter":
			if reasoningRequested && effort != "" {
				req.Reasoning = map[string]interface{}{"effort": effort}
			} else if off, ok := model.ThinkingLevelMap[goai.ThinkingOff]; ok && off != nil {
				req.Reasoning = map[string]interface{}{"effort": *off}
			}
		case "ant-ling":
			if reasoningRequested && effort != "" {
				req.Reasoning = map[string]interface{}{"effort": effort}
			}
		case "together":
			req.Reasoning = map[string]interface{}{"enabled": reasoningRequested && effort != ""}
			if reasoningRequested && effort != "" && (compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort) {
				req.ReasoningEffort = effort
			}
		default:
			if reasoningRequested && effort != "" && (compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort) {
				req.ReasoningEffort = effort
			} else if !reasoningRequested {
				if off, ok := model.ThinkingLevelMap[goai.ThinkingOff]; ok && off != nil && (compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort) {
					req.ReasoningEffort = *off
				}
			}
		}
	}

	useAnthropicCacheControl := cacheRetention != goai.CacheRetentionNone && compat.CacheControlFormat == "anthropic"
	cacheMarker := (*cacheControl)(nil)
	if useAnthropicCacheControl {
		cacheMarker = &cacheControl{Type: "ephemeral"}
		if cacheRetention == goai.CacheRetentionLong {
			cacheMarker.TTL = "1h"
		}
	}

	// Convert messages with compat awareness
	req.Messages = convertMessages(model, convCtx, &compat)
	if useAnthropicCacheControl {
		applyAnthropicCacheControl(req.Messages, cacheMarker)
	}

	// Convert tools
	if len(convCtx.Tools) > 0 {
		if compat.ZaiToolStream != nil && *compat.ZaiToolStream {
			t := true
			req.ToolStream = &t
		}
		strictMode := compat.SupportsStrictMode == nil || *compat.SupportsStrictMode
		for _, t := range convCtx.Tools {
			req.Tools = append(req.Tools, toolDef{
				Type:         "function",
				CacheControl: cacheMarker,
				Function: toolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
					Strict:      strictMode,
				},
			})
		}
	}

	return req
}

func applyAnthropicCacheControl(msgs []chatMessage, marker *cacheControl) {
	if marker == nil || len(msgs) == 0 {
		return
	}
	for i := range msgs {
		if msgs[i].Role != "system" && msgs[i].Role != "developer" {
			continue
		}
		if text, ok := msgs[i].Content.(string); ok {
			msgs[i].Content = []contentPart{{Type: "text", Text: text, CacheControl: marker}}
		}
		break
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		switch content := msgs[i].Content.(type) {
		case string:
			msgs[i].Content = []contentPart{{Type: "text", Text: content, CacheControl: marker}}
		case []contentPart:
			for j := range content {
				if content[j].Type == "text" {
					content[j].CacheControl = marker
					break
				}
			}
			msgs[i].Content = content
		}
		break
	}
}

func buildChatTemplateKwargs(model *goai.Model, opts *goai.StreamOptions, compat goai.OpenAICompletionsCompat, effort string) map[string]interface{} {
	if len(compat.ChatTemplateKwargs) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(compat.ChatTemplateKwargs))
	for key, value := range compat.ChatTemplateKwargs {
		resolved, ok := resolveChatTemplateKwargValue(model, opts, effort, value)
		if ok {
			out[key] = resolved
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveChatTemplateKwargValue(model *goai.Model, _ *goai.StreamOptions, effort string, value goai.ChatTemplateKwargValue) (interface{}, bool) {
	if value.Var == "" {
		return value.Value, true
	}
	if effort == "" && value.OmitWhenOff {
		return nil, false
	}
	switch value.Var {
	case "thinking.enabled":
		return effort != "", true
	case "thinking.effort":
		if effort != "" {
			return effort, true
		}
		if mapped, ok := model.ThinkingLevelMap[goai.ThinkingOff]; ok {
			if mapped == nil {
				return nil, false
			}
			return *mapped, true
		}
		return nil, false
	default:
		return value.Value, true
	}
}

func convertMessages(model *goai.Model, convCtx *goai.Context, compat *goai.OpenAICompletionsCompat) []chatMessage {
	var msgs []chatMessage

	// System prompt — use developer role for reasoning models if supported
	if convCtx.SystemPrompt != "" {
		role := "system"
		if model.Reasoning && compat.SupportsDeveloperRole != nil && *compat.SupportsDeveloperRole {
			role = "developer"
		}
		msgs = append(msgs, chatMessage{
			Role:    role,
			Content: goai.SanitizeSurrogates(convCtx.SystemPrompt),
		})
	}

	transformed := goai.TransformMessages(convCtx.Messages, model)
	var lastRole goai.Role

	for i := 0; i < len(transformed); i++ {
		m := transformed[i]

		// Insert synthetic assistant message after tool results if required
		if compat.RequiresAssistantAfterToolResult != nil && *compat.RequiresAssistantAfterToolResult &&
			lastRole == goai.RoleToolResult && m.Role == goai.RoleUser {
			msgs = append(msgs, chatMessage{Role: "assistant", Content: "I have processed the tool results."})
		}

		switch m.Role {
		case goai.RoleUser:
			// Check for image content
			hasImages := false
			for _, b := range m.Content {
				if b.Type == "image" {
					hasImages = true
					break
				}
			}

			if hasImages {
				// Multi-modal content array
				var parts []contentPart
				for _, b := range m.Content {
					switch b.Type {
					case "text":
						parts = append(parts, contentPart{
							Type: "text",
							Text: goai.SanitizeSurrogates(b.Text),
						})
					case "image":
						parts = append(parts, contentPart{
							Type: "image_url",
							ImageURL: &imageURL{
								URL: fmt.Sprintf("data:%s;base64,%s", b.MimeType, b.Data),
							},
						})
					}
				}
				if len(parts) > 0 {
					msgs = append(msgs, chatMessage{Role: "user", Content: parts})
				}
			} else {
				// Plain text
				text := extractTextContent(m.Content)
				msgs = append(msgs, chatMessage{Role: "user", Content: goai.SanitizeSurrogates(text)})
			}

		case goai.RoleAssistant:
			msg := chatMessage{Role: "assistant"}

			// Collect text and thinking
			var textParts []string
			var thinkingParts []string
			for _, c := range m.Content {
				switch c.Type {
				case "text":
					if c.Text != "" {
						textParts = append(textParts, c.Text)
					}
				case "thinking":
					if c.Thinking != "" {
						thinkingParts = append(thinkingParts, c.Thinking)
					}
				case "toolCall":
					argsJSON, _ := json.Marshal(c.Arguments)
					if c.ThoughtSignature != "" {
						var detail reasoningDetail
						if err := json.Unmarshal([]byte(c.ThoughtSignature), &detail); err == nil && isEncryptedReasoningDetail(detail) {
							msg.ReasoningDetails = append(msg.ReasoningDetails, detail)
						}
					}
					msg.ToolCalls = append(msg.ToolCalls, toolCallPart{
						ID:   c.ID,
						Type: "function",
						Function: toolCallFunction{
							Name:      c.Name,
							Arguments: string(argsJSON),
						},
					})
				}
			}

			// Handle thinking blocks
			if len(thinkingParts) > 0 && compat.RequiresThinkingAsText != nil && *compat.RequiresThinkingAsText {
				// Convert thinking to text content
				allText := joinStrings(thinkingParts)
				if len(textParts) > 0 {
					allText += "\n\n" + joinStrings(textParts)
				}
				msg.Content = goai.SanitizeSurrogates(allText)
			} else if len(textParts) > 0 {
				msg.Content = goai.SanitizeSurrogates(joinStrings(textParts))
			}

			// Skip empty assistant messages with no tool calls
			if msg.Content == nil && len(msg.ToolCalls) == 0 {
				continue
			}
			if msg.Content == nil {
				msg.Content = ""
			}
			msgs = append(msgs, msg)

		case goai.RoleToolResult:
			text := extractTextContent(m.Content)
			toolMsg := chatMessage{
				Role:       "tool",
				Content:    goai.SanitizeSurrogates(text),
				ToolCallID: m.ToolCallID,
			}
			// Some providers require the name field
			if compat.RequiresToolResultName != nil && *compat.RequiresToolResultName && m.ToolName != "" {
				toolMsg.Name = m.ToolName
			}
			msgs = append(msgs, toolMsg)

			// If tool result has images, add them as a follow-up user message
			var imageBlocks []contentPart
			for _, b := range m.Content {
				if b.Type == "image" {
					imageBlocks = append(imageBlocks, contentPart{
						Type: "image_url",
						ImageURL: &imageURL{
							URL: fmt.Sprintf("data:%s;base64,%s", b.MimeType, b.Data),
						},
					})
				}
			}
			if len(imageBlocks) > 0 {
				if compat.RequiresAssistantAfterToolResult != nil && *compat.RequiresAssistantAfterToolResult {
					msgs = append(msgs, chatMessage{Role: "assistant", Content: "I have processed the tool results."})
				}
				msgs = append(msgs, chatMessage{
					Role:    "user",
					Content: append([]contentPart{{Type: "text", Text: "Tool result image:"}}, imageBlocks...),
				})
			}
		}

		lastRole = m.Role
	}

	return msgs
}
func extractTextContent(blocks []goai.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

func joinStrings(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n"
		}
		result += p
	}
	return result
}

// --- SSE response processing ---

type sseChunk struct {
	ID      string      `json:"id"`
	Model   string      `json:"model,omitempty"`
	Choices []sseChoice `json:"choices"`
	Usage   *sseUsage   `json:"usage,omitempty"`
}

type sseChoice struct {
	Index        int       `json:"index"`
	Delta        sseDelta  `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
	Usage        *sseUsage `json:"usage,omitempty"`
}

type sseDelta struct {
	Role             string            `json:"role,omitempty"`
	Content          *string           `json:"content,omitempty"`
	ToolCalls        []sseToolCall     `json:"tool_calls,omitempty"`
	Reasoning        *string           `json:"reasoning,omitempty"`
	ReasoningContent *string           `json:"reasoning_content,omitempty"`
	ReasoningText    *string           `json:"reasoning_text,omitempty"`
	ReasoningDetails []reasoningDetail `json:"reasoning_details,omitempty"`
}

type reasoningDetail struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Data string `json:"data,omitempty"`
}

func isEncryptedReasoningDetail(detail reasoningDetail) bool {
	return detail.Type == "reasoning.encrypted" && detail.ID != "" && detail.Data != ""
}

type sseToolCall struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function sseToolFunction `json:"function"`
}

type sseToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type sseUsage struct {
	PromptTokens         int `json:"prompt_tokens"`
	CompletionTokens     int `json:"completion_tokens"`
	TotalTokens          int `json:"total_tokens"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  *struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
}

func processSSEStream(body io.Reader, model *goai.Model, ch chan<- goai.Event) {
	partial := &goai.Message{
		Role:     goai.RoleAssistant,
		Api:      model.Api,
		Provider: model.Provider,
		Model:    model.ID,
		Usage:    &goai.Usage{},
	}

	ch <- &goai.StartEvent{Partial: partial}

	// Track active tool calls for argument accumulation
	type activeToolCall struct {
		index      int
		id         string
		name       string
		argsBuf    string
		contentIdx int
	}
	var activeTools []activeToolCall
	pendingReasoningDetailsByToolCallID := map[string]string{}
	var finishReason *string

	events := sse.Parse(body)
	for evt := range events {
		if evt.Event == sse.EventError {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", evt.Data)}
			return
		}
		if evt.Data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
			continue
		}

		if chunk.ID != "" {
			partial.ResponseID = chunk.ID
		}
		if chunk.Model != "" && chunk.Model != model.ID && partial.ResponseModel == "" {
			partial.ResponseModel = chunk.Model
		}

		// Update usage
		if chunk.Usage != nil {
			applyUsage(partial.Usage, chunk.Usage, model)
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if chunk.Usage == nil && choice.Usage != nil {
			applyUsage(partial.Usage, choice.Usage, model)
		}
		delta := choice.Delta

		if choice.FinishReason != nil {
			finishReason = choice.FinishReason
		}

		// Text content
		if delta.Content != nil && *delta.Content != "" {
			if len(partial.Content) == 0 || partial.Content[len(partial.Content)-1].Type != "text" {
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				ch <- &goai.TextStartEvent{
					ContentIndex: len(partial.Content) - 1,
					Partial:      partial,
				}
			}
			idx := len(partial.Content) - 1
			partial.Content[idx].Text += *delta.Content
			ch <- &goai.TextDeltaEvent{
				ContentIndex: idx,
				Delta:        *delta.Content,
				Partial:      partial,
			}
		}

		// Thinking/reasoning content — check fields in priority order
		// (reasoning_content for llama.cpp, reasoning for other endpoints, reasoning_text)
		var reasoningDelta string
		if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
			reasoningDelta = *delta.ReasoningContent
		} else if delta.Reasoning != nil && *delta.Reasoning != "" {
			reasoningDelta = *delta.Reasoning
		} else if delta.ReasoningText != nil && *delta.ReasoningText != "" {
			reasoningDelta = *delta.ReasoningText
		}
		if reasoningDelta != "" {
			if len(partial.Content) == 0 || partial.Content[len(partial.Content)-1].Type != "thinking" {
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
				ch <- &goai.ThinkingStartEvent{
					ContentIndex: len(partial.Content) - 1,
					Partial:      partial,
				}
			}
			idx := len(partial.Content) - 1
			partial.Content[idx].Thinking += reasoningDelta
			ch <- &goai.ThinkingDeltaEvent{
				ContentIndex: idx,
				Delta:        reasoningDelta,
				Partial:      partial,
			}
		}

		// Tool calls
		for _, tc := range delta.ToolCalls {
			// Find or create active tool call
			var at *activeToolCall
			for i := range activeTools {
				if activeTools[i].index == tc.Index {
					at = &activeTools[i]
					break
				}
			}
			if at == nil {
				contentIdx := len(partial.Content)
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall",
					ID:   tc.ID,
					Name: tc.Function.Name,
				})
				activeTools = append(activeTools, activeToolCall{
					index:      tc.Index,
					id:         tc.ID,
					name:       tc.Function.Name,
					contentIdx: contentIdx,
				})
				at = &activeTools[len(activeTools)-1]
				ch <- &goai.ToolCallStartEvent{
					ContentIndex: contentIdx,
					Partial:      partial,
				}
			}

			// Accumulate arguments
			if tc.Function.Arguments != "" {
				at.argsBuf += tc.Function.Arguments
				ch <- &goai.ToolCallDeltaEvent{
					ContentIndex: at.contentIdx,
					Delta:        tc.Function.Arguments,
					Partial:      partial,
				}
			}

			// Update name/id if provided
			if tc.ID != "" {
				at.id = tc.ID
				partial.Content[at.contentIdx].ID = tc.ID
				if sig := pendingReasoningDetailsByToolCallID[tc.ID]; sig != "" {
					partial.Content[at.contentIdx].ThoughtSignature = sig
				}
			}
			if tc.Function.Name != "" {
				at.name = tc.Function.Name
				partial.Content[at.contentIdx].Name = tc.Function.Name
			}
		}

		// Encrypted reasoning details → attach as thoughtSignature on matching tool calls.
		// Some OpenAI-compatible streams emit reasoning_details before the matching
		// tool call ID is fully available, so retain validated encrypted details and
		// attach them when the tool call arrives later.
		for _, detail := range delta.ReasoningDetails {
			if !isEncryptedReasoningDetail(detail) {
				continue
			}
			sigBytes, _ := json.Marshal(detail)
			sig := string(sigBytes)
			matched := false
			for i := range partial.Content {
				if partial.Content[i].Type == "toolCall" && partial.Content[i].ID == detail.ID {
					partial.Content[i].ThoughtSignature = sig
					matched = true
					break
				}
			}
			if !matched {
				pendingReasoningDetailsByToolCallID[detail.ID] = sig
			}
		}
	}

	// Close any open text blocks
	for i, c := range partial.Content {
		if c.Type == "text" {
			ch <- &goai.TextEndEvent{ContentIndex: i, Content: c.Text, Partial: partial}
		}
		if c.Type == "thinking" {
			ch <- &goai.ThinkingEndEvent{ContentIndex: i, Content: c.Thinking, Partial: partial}
		}
	}

	// Close tool calls and parse arguments
	for _, at := range activeTools {
		args, _ := jsonparse.ParsePartialJSON(at.argsBuf)
		if args == nil {
			args = map[string]interface{}{}
		}
		partial.Content[at.contentIdx].Arguments = args
		ch <- &goai.ToolCallEndEvent{
			ContentIndex: at.contentIdx,
			ToolCall: goai.ToolCall{
				Type:      "toolCall",
				ID:        at.id,
				Name:      at.name,
				Arguments: args,
			},
			Partial: partial,
		}
	}

	// Determine stop reason
	partial.Timestamp = time.Now().UnixMilli()
	reason := goai.StopReasonStop
	if finishReason != nil {
		switch *finishReason {
		case "stop", "end":
			reason = goai.StopReasonStop
		case "length":
			reason = goai.StopReasonLength
		case "tool_calls", "function_call":
			reason = goai.StopReasonToolUse
		case "content_filter", "network_error":
			reason = goai.StopReasonError
			partial.ErrorMessage = "Provider finish_reason: " + *finishReason
		default:
			reason = goai.StopReasonError
			partial.ErrorMessage = "Provider finish_reason: " + *finishReason
		}
	}
	partial.StopReason = reason

	ch <- &goai.DoneEvent{Reason: reason, Message: partial}
}

func applyUsage(usage *goai.Usage, raw *sseUsage, model *goai.Model) {
	if usage == nil || raw == nil {
		return
	}
	reportedCached := raw.PromptCacheHitTokens
	cacheWrite := 0
	if raw.PromptTokensDetails != nil {
		if raw.PromptTokensDetails.CachedTokens > 0 {
			reportedCached = raw.PromptTokensDetails.CachedTokens
		}
		cacheWrite = raw.PromptTokensDetails.CacheWriteTokens
	}
	cacheRead := reportedCached
	if cacheWrite > 0 && cacheRead >= cacheWrite {
		cacheRead -= cacheWrite
	}
	input := raw.PromptTokens - cacheRead - cacheWrite
	if input < 0 {
		input = 0
	}
	usage.Input = input
	usage.Output = raw.CompletionTokens
	usage.CacheRead = cacheRead
	usage.CacheWrite = cacheWrite
	usage.TotalTokens = input + raw.CompletionTokens + cacheRead + cacheWrite
	if raw.TotalTokens > 0 {
		usage.TotalTokens = raw.TotalTokens
	}
	computeCosts(usage, model)
}

func computeCosts(usage *goai.Usage, model *goai.Model) {
	m := 1_000_000.0
	usage.Cost.Input = float64(usage.Input) * model.Cost.Input / m
	usage.Cost.Output = float64(usage.Output) * model.Cost.Output / m
	usage.Cost.CacheRead = float64(usage.CacheRead) * model.Cost.CacheRead / m
	usage.Cost.CacheWrite = float64(usage.CacheWrite) * model.Cost.CacheWrite / m
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
}
