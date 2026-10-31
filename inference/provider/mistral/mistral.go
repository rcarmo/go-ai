// Package mistral implements the Mistral Conversations API provider.
//
// Mistral uses an OpenAI-compatible chat completions protocol with minor
// extensions (promptMode, reasoning). This provider hits the Mistral REST
// API directly with SSE streaming.
package mistral

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/errortext"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/sse"
)

const defaultBaseURL = "https://api.mistral.ai/v1"

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiMistralConversations,
		Stream:       streamMistral,
		StreamSimple: streamMistralSimple,
	})
}

func streamMistralSimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamMistral(ctx, model, convCtx, opts)
}

func streamMistral(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)
	send := goai.NewAssistantEventSender(ch)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "mistral-conversations", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("No API key for provider: %s", model.Provider)})
			return
		}

		body := buildRequest(model, convCtx, opts)
		payload, err := goai.InvokeOnPayload(opts, body, model)
		if err != nil {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
			return
		}
		bodyJSON, err := marshalMistralPayload(payload)
		if err != nil {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
			return
		}

		baseURL := model.BaseURL
		if baseURL == "" {
			baseURL = defaultBaseURL
		}

		// Timeout bounds header admission only. Keep caller cancellation on the
		// request for the entire body, but disarm this timer before streaming.
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		timeoutMs := 60000
		if opts != nil && opts.TimeoutMs != nil {
			timeoutMs = *opts.TimeoutMs
		}

		req, err := http.NewRequestWithContext(requestCtx, "POST", baseURL+"/chat/completions", bytes.NewReader(bodyJSON))
		if err != nil {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
			return
		}

		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(bodyJSON)), nil }

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "text/event-stream")
		if shouldUsePromptCaching(opts) && !hasHeaderOverride(model.Headers, "x-affinity") && (opts == nil || !hasHeaderOverride(opts.Headers, "x-affinity")) {
			req.Header.Set("x-affinity", opts.SessionID)
		}

		goai.ApplyDefaultHeaders(req.Header, model.Headers)
		goai.ApplyDefaultHeaders(req.Header, goai.RequestIdentityHeaders(model, opts))
		if opts != nil {
			goai.ApplyHeaders(req.Header, opts.Headers)
			goai.SuppressHeaders(req.Header, opts.SuppressHeaders)
		}

		retryCfg := goai.RetryConfigFromOptions(opts)
		client := retryCfg.NewHTTPClient()
		client.Timeout = 0 // http.Client.Timeout would also cancel the SSE body
		var timerMu sync.Mutex
		headerDone, timedOut := false, false
		var timer *time.Timer
		if timeoutMs > 0 {
			timer = time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
				timerMu.Lock()
				defer timerMu.Unlock()
				if !headerDone {
					timedOut = true
					cancel()
				}
			})
		}
		goai.GetLogger().Debug("HTTP request", "url", req.URL.String(), "provider", model.Provider, "model", model.ID, "retries", retryCfg.MaxRetries)
		resp, err := goai.DoWithRetry(requestCtx, client, req, retryCfg)
		timerMu.Lock()
		headerDone = true
		if timer != nil {
			timer.Stop()
		}
		wasTimeout := timedOut
		timerMu.Unlock()
		if err != nil {
			if wasTimeout && ctx.Err() == nil {
				//lint:ignore ST1005 Preserve official1.2.0 timeout diagnostic.
				err = fmt.Errorf("Mistral response headers timed out after %dms", timeoutMs)
			}
			if ctx.Err() != nil {
				goai.GetLogger().Debug("request aborted", "provider", model.Provider, "model", model.ID)
				send(&goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()})
			} else {
				goai.GetLogger().Warn("network error", "provider", model.Provider, "model", model.ID, "error", err)
				send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
			}
			return
		}
		defer resp.Body.Close()

		goai.InvokeOnResponse(opts, resp, model)

		if resp.StatusCode != 200 {
			goai.GetLogger().Warn("HTTP error response", "status", resp.StatusCode, "provider", model.Provider, "model", model.ID)
			failure := readMistralHTTPError(resp)
			reason := goai.StopReasonError
			if ctx.Err() != nil {
				reason = goai.StopReasonAborted
			}
			send(&goai.ErrorEvent{
				Reason: reason,
				Err:    failure,
			})
			return
		}

		processSSEStream(resp.Body, model, send)
	}()

	return ch
}

// --- Request ---

type mistralRequest struct {
	Model           string        `json:"model"`
	Messages        []mistralMsg  `json:"messages"`
	Stream          bool          `json:"stream"`
	Temperature     *float64      `json:"temperature,omitempty"`
	MaxTokens       *int          `json:"max_tokens,omitempty"`
	Tools           []mistralTool `json:"tools,omitempty"`
	PromptMode      string        `json:"prompt_mode,omitempty"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	PromptCacheKey  string        `json:"prompt_cache_key,omitempty"`
}

type mistralMsg struct {
	Role       string            `json:"role"`
	Content    interface{}       `json:"content,omitempty"`
	Prefix     *bool             `json:"prefix,omitempty"`
	ToolCalls  []mistralToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Name       string            `json:"name,omitempty"`
}

type mistralToolCall struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function mistralToolCallFunc `json:"function"`
}

type mistralToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type mistralTool struct {
	Type     string             `json:"type"`
	Function mistralToolFuncDef `json:"function"`
}

type mistralToolFuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}

func buildRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) mistralRequest {
	req := mistralRequest{
		Model:  model.ID,
		Stream: true,
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		req.MaxTokens = goai.ClampStreamMaxTokensPtr(model, convCtx, opts)
		if opts.SessionID != "" && opts.CacheRetention != goai.CacheRetentionNone {
			req.PromptCacheKey = opts.SessionID
		}
		if opts.Reasoning != nil && *opts.Reasoning != "" && model.Reasoning {
			level := goai.ModelThinkingLevel(*opts.Reasoning)
			if mapped, ok := goai.MapThinkingLevel(model, level); ok && mapped != "none" {
				if usesReasoningEffort(model) {
					req.ReasoningEffort = mapped
				} else {
					req.PromptMode = "reasoning"
				}
			}
		}
	}

	// System prompt
	if convCtx.SystemPrompt != "" {
		req.Messages = append(req.Messages, mistralMsg{
			Role:    "system",
			Content: goai.SanitizeSurrogates(convCtx.SystemPrompt),
		})
	}

	// Messages
	transformed := goai.TransformMessages(convCtx.Messages, model)
	toolCallIDMap := make(map[string]string)
	normalizeID := newMistralToolCallIDNormalizer()
	for _, msg := range transformed {
		switch msg.Role {
		case goai.RoleUser:
			text := ""
			for _, b := range msg.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			req.Messages = append(req.Messages, mistralMsg{Role: "user", Content: goai.SanitizeSurrogates(text)})

		case goai.RoleAssistant:
			prefix := false
			m := mistralMsg{Role: "assistant", Prefix: &prefix}
			var parts []mistralContentPart
			for _, b := range msg.Content {
				switch b.Type {
				case "text":
					if errortext.Trim(b.Text) != "" {
						parts = append(parts, mistralContentPart{Type: "text", Text: goai.SanitizeSurrogates(b.Text)})
					}
				case "thinking":
					if errortext.Trim(b.Thinking) != "" {
						parts = append(parts, mistralContentPart{Type: "thinking", Thinking: []mistralThinkingText{{Type: "text", Text: goai.SanitizeSurrogates(b.Thinking)}}})
					}
				case "toolCall":
					argsJSON, _ := json.Marshal(b.Arguments)
					normID := normalizeID(b.ID)
					if normID != b.ID {
						toolCallIDMap[b.ID] = normID
					}
					m.ToolCalls = append(m.ToolCalls, mistralToolCall{
						ID:       normID,
						Type:     "function",
						Function: mistralToolCallFunc{Name: b.Name, Arguments: string(argsJSON)},
					})
				}
			}
			if len(parts) > 0 {
				m.Content = parts
			}
			if len(parts) > 0 || len(m.ToolCalls) > 0 {
				req.Messages = append(req.Messages, m)
			}

		case goai.RoleToolResult:
			text := ""
			for _, b := range msg.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			tcID := msg.ToolCallID
			if mapped, ok := toolCallIDMap[tcID]; ok {
				tcID = mapped
			}
			req.Messages = append(req.Messages, mistralMsg{
				Role:       "tool",
				Content:    goai.SanitizeSurrogates(text),
				ToolCallID: tcID,
				Name:       msg.ToolName,
			})
		}
	}

	// Tools
	for _, t := range convCtx.Tools {
		parameters := t.Parameters
		strictTool := false
		if strict, err := goai.ResolveJSONSchemaStrictSampling(t, true); err == nil && strict != nil {
			strictTool = *strict
			if *strict {
				if strictParameters, err := goai.JSONSchemaToolParameters(t, true); err == nil {
					parameters = strictParameters
				}
			}
		}
		req.Tools = append(req.Tools, mistralTool{
			Type: "function",
			Function: mistralToolFuncDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  parameters,
				Strict:      strictTool,
			},
		})
	}

	return req
}

// --- SSE processing ---
// Mistral uses the same SSE format as OpenAI Chat Completions

type sseChunk struct {
	ID      string      `json:"id,omitempty"`
	Choices []sseChoice `json:"choices"`
	Usage   *sseUsage   `json:"usage,omitempty"`
}

type sseChoice struct {
	Delta        sseDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type sseDelta struct {
	Content   json.RawMessage `json:"content,omitempty"`
	Reasoning *string         `json:"reasoning_content,omitempty"`
	ToolCalls []sseToolCall   `json:"tool_calls,omitempty"`
}

type sseToolCall struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Function sseToolCallFunc `json:"function"`
}

type sseToolCallFunc struct {
	Name      string              `json:"name,omitempty"`
	Arguments mistralArgumentText `json:"arguments,omitempty"`
}

type sseUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	PromptTokenDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_token_details"`
	NumCachedTokens int `json:"num_cached_tokens"`
}

func shouldUsePromptCaching(opts *goai.StreamOptions) bool {
	if opts == nil || opts.SessionID == "" {
		return false
	}
	return opts.CacheRetention != goai.CacheRetentionNone
}

func hasHeaderOverride(headers map[string]string, target string) bool {
	for name := range headers {
		if strings.EqualFold(name, target) {
			return true
		}
	}
	return false
}

func getMistralCachedPromptTokens(usage *sseUsage) int {
	if usage == nil {
		return 0
	}
	cached := usage.NumCachedTokens
	if usage.PromptTokensDetails != nil {
		cached = usage.PromptTokensDetails.CachedTokens
	} else if usage.PromptTokenDetails != nil {
		cached = usage.PromptTokenDetails.CachedTokens
	}
	return cached
}

func processSSEStream(body io.Reader, model *goai.Model, send func(goai.Event)) {
	partial := &goai.Message{
		Role:       goai.RoleAssistant,
		Api:        model.Api,
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      &goai.Usage{},
		StopReason: goai.StopReasonPending,
	}

	send(&goai.StartEvent{Partial: partial})

	type activeTC struct {
		index      int
		id         string
		name       string
		arguments  jsonparse.ToolCallArgumentsStream
		contentIdx int
	}
	// Argument streams contain non-copyable builders; keep their addresses stable.
	var activeTools []*activeTC
	var finishReason *string

	currentContent := -1
	finishContent := func() {
		if currentContent < 0 {
			return
		}
		block := partial.Content[currentContent]
		if block.Type == "text" {
			send(&goai.TextEndEvent{ContentIndex: currentContent, Content: block.Text, Partial: partial})
		} else {
			send(&goai.ThinkingEndEvent{ContentIndex: currentContent, Content: block.Thinking, Partial: partial})
		}
		currentContent = -1
	}
	appendContent := func(kind, text string) {
		if text == "" {
			return
		}
		if currentContent < 0 || partial.Content[currentContent].Type != kind {
			finishContent()
			currentContent = len(partial.Content)
			partial.Content = append(partial.Content, goai.ContentBlock{Type: kind})
			if kind == "text" {
				send(&goai.TextStartEvent{ContentIndex: currentContent, Partial: partial})
			} else {
				send(&goai.ThinkingStartEvent{ContentIndex: currentContent, Partial: partial})
			}
		}
		if kind == "text" {
			partial.Content[currentContent].Text += text
			send(&goai.TextDeltaEvent{ContentIndex: currentContent, Delta: text, Partial: partial})
		} else {
			partial.Content[currentContent].Thinking += text
			send(&goai.ThinkingDeltaEvent{ContentIndex: currentContent, Delta: text, Partial: partial})
		}
	}

	events := sse.Parse(body)
	for evt := range events {
		if evt.Event == sse.EventError {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", evt.Data)})
			return
		}
		if evt.Data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if json.Unmarshal([]byte(evt.Data), &chunk) != nil {
			continue
		}

		if partial.ResponseID == "" && chunk.ID != "" {
			partial.ResponseID = chunk.ID
		}
		if chunk.Usage != nil {
			cached := getMistralCachedPromptTokens(chunk.Usage)
			if cached > chunk.Usage.PromptTokens {
				cached = chunk.Usage.PromptTokens
			}
			if cached < 0 {
				cached = 0
			}
			partial.Usage.Input = max(0, chunk.Usage.PromptTokens-cached)
			partial.Usage.Output = chunk.Usage.CompletionTokens
			partial.Usage.CacheRead = cached
			partial.Usage.TotalTokens = chunk.Usage.TotalTokens
			if partial.Usage.TotalTokens == 0 {
				partial.Usage.TotalTokens = partial.Usage.Input + partial.Usage.Output + partial.Usage.CacheRead + partial.Usage.CacheWrite
			}
			partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		if choice.FinishReason != nil {
			finishReason = choice.FinishReason
			partial.RawStopReason = *choice.FinishReason
		}

		deltas := mistralContentDeltas(delta.Content)
		hasThinking := false
		for _, part := range deltas {
			appendContent(part.kind, part.text)
			hasThinking = hasThinking || part.kind == "thinking"
		}
		// Preserve legacy native reasoning_content support when canonical
		// thinking chunks are absent; canonical content is never duplicated.
		if !hasThinking && delta.Reasoning != nil {
			appendContent("thinking", goai.SanitizeSurrogates(*delta.Reasoning))
		}

		// Tool calls
		for _, tc := range delta.ToolCalls {
			finishContent()
			var at *activeTC
			for i := range activeTools {
				if activeTools[i].index == tc.Index || (tc.ID != "" && activeTools[i].id == tc.ID) {
					at = activeTools[i]
					break
				}
			}
			if at == nil {
				contentIdx := len(partial.Content)
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall", ID: tc.ID, Name: tc.Function.Name,
				})
				activeTools = append(activeTools, &activeTC{
					index: tc.Index, id: tc.ID, name: tc.Function.Name, contentIdx: contentIdx,
				})
				at = activeTools[len(activeTools)-1]
				partial.Content[contentIdx].SetStreamingArguments(at.arguments.Append(""), "")
				send(&goai.ToolCallStartEvent{ContentIndex: contentIdx, Partial: partial})
			}
			if tc.Function.Arguments.present {
				delta := tc.Function.Arguments.text
				partial.Content[at.contentIdx].SetStreamingArguments(at.arguments.Append(delta), "")
				send(&goai.ToolCallDeltaEvent{ContentIndex: at.contentIdx, Delta: delta, Partial: partial})
			}
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

	finishContent()

	// Close tool calls
	for i := range activeTools {
		at := activeTools[i]
		args, argumentsError := at.arguments.Finish()
		partial.Content[at.contentIdx].SetStreamingArguments(args, argumentsError)
		call := goai.ToolCall{Type: "toolCall", ID: at.id, Name: at.name}
		call.SetStreamingArguments(args, argumentsError)
		send(&goai.ToolCallEndEvent{
			ContentIndex: at.contentIdx,
			ToolCall:     call,
			Partial:      partial,
		})
	}

	partial.Timestamp = time.Now().UnixMilli()
	reason := goai.StopReasonStop
	if finishReason != nil {
		switch *finishReason {
		case "stop":
			reason = goai.StopReasonStop
		case "length", "model_length":
			reason = goai.StopReasonLength
		case "tool_calls":
			reason = goai.StopReasonToolUse
		case "error":
			reason = goai.StopReasonError
			partial.ErrorMessage = "Provider stopped with: error (server error)"
		default:
			reason = goai.StopReasonError
			partial.ErrorMessage = "Provider stopped with: " + *finishReason
		}
	}
	if finishReason == nil {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = "Mistral stream ended without a finish reason"
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("mistral stream ended without a finish reason")})
		return
	}
	partial.StopReason = reason

	send(&goai.DoneEvent{Reason: reason, Message: partial})
}

func usesReasoningEffort(model *goai.Model) bool {
	if model == nil {
		return false
	}
	switch model.ID {
	case "mistral-small-2603", "mistral-small-latest", "mistral-medium-3.5":
		return true
	default:
		return false
	}
}

const mistralToolCallIDLength = 9
