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
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/eventstream"
	"github.com/rcarmo/go-ai/internal/jsonparse"
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
		if apiKey == "" {
			ch <- &goai.ErrorEvent{
				Reason: goai.StopReasonError,
				Err:    fmt.Errorf("no API key for provider %s", model.Provider),
			}
			return
		}

		// Build request body
		body := buildRequestBody(model, convCtx, opts)

		bodyJSON, err := json.Marshal(body)
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		url := model.BaseURL + "/chat/completions"
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "text/event-stream")

		// Session affinity headers for prompt caching
		compat := goai.DetectCompat(model.BaseURL)
		if compat.SendSessionAffinityHeaders != nil && *compat.SendSessionAffinityHeaders && opts != nil && opts.SessionID != "" {
			req.Header.Set("x-session-id", opts.SessionID)
			req.Header.Set("x-client-request-id", opts.SessionID)
			req.Header.Set("x-session-affinity", opts.SessionID)
		}

		// Apply custom headers
		if opts != nil {
			for k, v := range opts.Headers {
				req.Header.Set(k, v)
			}
		}
		for k, v := range model.Headers {
			if req.Header.Get(k) == "" {
				req.Header.Set(k, v)
			}
		}

		client := &http.Client{Timeout: 10 * time.Minute}
		goai.GetLogger().Debug("HTTP request", "url", req.URL.String(), "provider", model.Provider, "model", model.ID)
		resp, err := client.Do(req)
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
	Model              string         `json:"model"`
	Messages           []chatMessage  `json:"messages"`
	Stream             bool           `json:"stream"`
	StreamOptions      *streamOpts    `json:"stream_options,omitempty"`
	Temperature        *float64       `json:"temperature,omitempty"`
	MaxTokens          *int           `json:"max_tokens,omitempty"`
	MaxCompletionToks  *int           `json:"max_completion_tokens,omitempty"`
	Tools              []toolDef      `json:"tools,omitempty"`
	ReasoningEffort    string         `json:"reasoning_effort,omitempty"`
	Store              *bool          `json:"store,omitempty"`
}

type streamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string       `json:"role"`
	Content    interface{}  `json:"content"`         // string or []contentPart
	ToolCalls  []toolCallPart `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	Name       string       `json:"name,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type toolDef struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
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
	// Detect compat flags from base URL
	compat := goai.DetectCompat(model.BaseURL)

	req := chatRequest{
		Model:  model.ID,
		Stream: true,
	}

	// Stream options — some providers don't support include_usage
	if compat.SupportsUsageInStreaming == nil || *compat.SupportsUsageInStreaming {
		req.StreamOptions = &streamOpts{IncludeUsage: true}
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

	// Reasoning effort
	if opts != nil && opts.Reasoning != nil && (compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort) {
		req.ReasoningEffort = string(*opts.Reasoning)
	}

	// Convert messages with compat awareness
	req.Messages = convertMessages(model, convCtx, &compat)

	// Convert tools
	if len(convCtx.Tools) > 0 {
		strictMode := compat.SupportsStrictMode == nil || *compat.SupportsStrictMode
		for _, t := range convCtx.Tools {
			req.Tools = append(req.Tools, toolDef{
				Type: "function",
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
					Role: "user",
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
	ID      string         `json:"id"`
	Choices []sseChoice    `json:"choices"`
	Usage   *sseUsage      `json:"usage,omitempty"`
}

type sseChoice struct {
	Index        int       `json:"index"`
	Delta        sseDelta  `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type sseDelta struct {
	Role      string         `json:"role,omitempty"`
	Content   *string        `json:"content,omitempty"`
	ToolCalls []sseToolCall  `json:"tool_calls,omitempty"`
	Reasoning *string        `json:"reasoning,omitempty"`
}

type sseToolCall struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function sseToolFunction  `json:"function"`
}

type sseToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type sseUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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
		index    int
		id       string
		name     string
		argsBuf  string
		contentIdx int
	}
	var activeTools []activeToolCall
	var finishReason *string

	events := eventstream.Parse(body)
	for sse := range events {
		if sse.Data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(sse.Data), &chunk); err != nil {
			continue
		}

		// Update usage
		if chunk.Usage != nil {
			partial.Usage.Input = chunk.Usage.PromptTokens
			partial.Usage.Output = chunk.Usage.CompletionTokens
			partial.Usage.TotalTokens = chunk.Usage.TotalTokens
			computeCosts(partial.Usage, model)
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
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

		// Thinking/reasoning content
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			if len(partial.Content) == 0 || partial.Content[len(partial.Content)-1].Type != "thinking" {
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
				ch <- &goai.ThinkingStartEvent{
					ContentIndex: len(partial.Content) - 1,
					Partial:      partial,
				}
			}
			idx := len(partial.Content) - 1
			partial.Content[idx].Thinking += *delta.Reasoning
			ch <- &goai.ThinkingDeltaEvent{
				ContentIndex: idx,
				Delta:        *delta.Reasoning,
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
			}
			if tc.Function.Name != "" {
				at.name = tc.Function.Name
				partial.Content[at.contentIdx].Name = tc.Function.Name
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
		case "stop":
			reason = goai.StopReasonStop
		case "length":
			reason = goai.StopReasonLength
		case "tool_calls":
			reason = goai.StopReasonToolUse
		}
	}
	partial.StopReason = reason

	ch <- &goai.DoneEvent{Reason: reason, Message: partial}
}

func computeCosts(usage *goai.Usage, model *goai.Model) {
	m := 1_000_000.0
	usage.Cost.Input = float64(usage.Input) * model.Cost.Input / m
	usage.Cost.Output = float64(usage.Output) * model.Cost.Output / m
	usage.Cost.CacheRead = float64(usage.CacheRead) * model.Cost.CacheRead / m
	usage.Cost.CacheWrite = float64(usage.CacheWrite) * model.Cost.CacheWrite / m
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
}
