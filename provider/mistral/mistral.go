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
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/eventstream"
	"github.com/rcarmo/go-ai/internal/jsonparse"
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

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "mistral-conversations", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("no API key for Mistral")}
			return
		}

		body := buildRequest(model, convCtx, opts)
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		baseURL := model.BaseURL
		if baseURL == "" {
			baseURL = defaultBaseURL
		}

		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/chat/completions", bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "text/event-stream")

		if opts != nil {
			for k, v := range opts.Headers {
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

// --- Request ---

type mistralRequest struct {
	Model       string         `json:"model"`
	Messages    []mistralMsg   `json:"messages"`
	Stream      bool           `json:"stream"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   *int           `json:"max_tokens,omitempty"`
	Tools       []mistralTool  `json:"tools,omitempty"`
	PromptMode  string         `json:"prompt_mode,omitempty"`
}

type mistralMsg struct {
	Role       string             `json:"role"`
	Content    interface{}        `json:"content"`
	ToolCalls  []mistralToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	Name       string             `json:"name,omitempty"`
}

type mistralToolCall struct {
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
}

func buildRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) mistralRequest {
	req := mistralRequest{
		Model:  model.ID,
		Stream: true,
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		req.MaxTokens = opts.MaxTokens
		if opts.Reasoning != nil && *opts.Reasoning != "" {
			req.PromptMode = "reasoning"
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
			m := mistralMsg{Role: "assistant"}
			var texts []string
			for _, b := range msg.Content {
				switch b.Type {
				case "text":
					texts = append(texts, b.Text)
				case "thinking":
					texts = append(texts, b.Thinking)
				case "toolCall":
					argsJSON, _ := json.Marshal(b.Arguments)
					m.ToolCalls = append(m.ToolCalls, mistralToolCall{
						ID:   b.ID,
						Type: "function",
						Function: mistralToolCallFunc{Name: b.Name, Arguments: string(argsJSON)},
					})
				}
			}
			if len(texts) > 0 {
				combined := ""
				for _, t := range texts {
					combined += t
				}
				m.Content = goai.SanitizeSurrogates(combined)
			}
			req.Messages = append(req.Messages, m)

		case goai.RoleToolResult:
			text := ""
			for _, b := range msg.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			req.Messages = append(req.Messages, mistralMsg{
				Role:       "tool",
				Content:    goai.SanitizeSurrogates(text),
				ToolCallID: msg.ToolCallID,
				Name:       msg.ToolName,
			})
		}
	}

	// Tools
	for _, t := range convCtx.Tools {
		req.Tools = append(req.Tools, mistralTool{
			Type: "function",
			Function: mistralToolFuncDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	return req
}

// --- SSE processing ---
// Mistral uses the same SSE format as OpenAI Chat Completions

type sseChunk struct {
	Choices []sseChoice `json:"choices"`
	Usage   *sseUsage   `json:"usage,omitempty"`
}

type sseChoice struct {
	Delta        sseDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type sseDelta struct {
	Content   *string        `json:"content,omitempty"`
	Reasoning *string        `json:"reasoning_content,omitempty"`
	ToolCalls []sseToolCall  `json:"tool_calls,omitempty"`
}

type sseToolCall struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Function sseToolCallFunc `json:"function"`
}

type sseToolCallFunc struct {
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

	type activeTC struct {
		index      int
		id         string
		name       string
		argsBuf    string
		contentIdx int
	}
	var activeTools []activeTC
	var finishReason *string

	events := eventstream.Parse(body)
	for sse := range events {
		if sse.Data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if json.Unmarshal([]byte(sse.Data), &chunk) != nil {
			continue
		}

		if chunk.Usage != nil {
			partial.Usage.Input = chunk.Usage.PromptTokens
			partial.Usage.Output = chunk.Usage.CompletionTokens
			partial.Usage.TotalTokens = chunk.Usage.TotalTokens
			partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		if choice.FinishReason != nil {
			finishReason = choice.FinishReason
		}

		// Text
		if delta.Content != nil && *delta.Content != "" {
			if len(partial.Content) == 0 || partial.Content[len(partial.Content)-1].Type != "text" {
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				ch <- &goai.TextStartEvent{ContentIndex: len(partial.Content) - 1, Partial: partial}
			}
			idx := len(partial.Content) - 1
			partial.Content[idx].Text += *delta.Content
			ch <- &goai.TextDeltaEvent{ContentIndex: idx, Delta: *delta.Content, Partial: partial}
		}

		// Reasoning
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			if len(partial.Content) == 0 || partial.Content[len(partial.Content)-1].Type != "thinking" {
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
				ch <- &goai.ThinkingStartEvent{ContentIndex: len(partial.Content) - 1, Partial: partial}
			}
			idx := len(partial.Content) - 1
			partial.Content[idx].Thinking += *delta.Reasoning
			ch <- &goai.ThinkingDeltaEvent{ContentIndex: idx, Delta: *delta.Reasoning, Partial: partial}
		}

		// Tool calls
		for _, tc := range delta.ToolCalls {
			var at *activeTC
			for i := range activeTools {
				if activeTools[i].index == tc.Index {
					at = &activeTools[i]
					break
				}
			}
			if at == nil {
				contentIdx := len(partial.Content)
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall", ID: tc.ID, Name: tc.Function.Name,
				})
				activeTools = append(activeTools, activeTC{
					index: tc.Index, id: tc.ID, name: tc.Function.Name, contentIdx: contentIdx,
				})
				at = &activeTools[len(activeTools)-1]
				ch <- &goai.ToolCallStartEvent{ContentIndex: contentIdx, Partial: partial}
			}
			if tc.Function.Arguments != "" {
				at.argsBuf += tc.Function.Arguments
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: at.contentIdx, Delta: tc.Function.Arguments, Partial: partial}
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

	// Close text/thinking blocks
	for i, c := range partial.Content {
		if c.Type == "text" {
			ch <- &goai.TextEndEvent{ContentIndex: i, Content: c.Text, Partial: partial}
		}
		if c.Type == "thinking" {
			ch <- &goai.ThinkingEndEvent{ContentIndex: i, Content: c.Thinking, Partial: partial}
		}
	}

	// Close tool calls
	for _, at := range activeTools {
		args, _ := jsonparse.ParsePartialJSON(at.argsBuf)
		if args == nil {
			args = map[string]interface{}{}
		}
		partial.Content[at.contentIdx].Arguments = args
		ch <- &goai.ToolCallEndEvent{
			ContentIndex: at.contentIdx,
			ToolCall: goai.ToolCall{
				Type: "toolCall", ID: at.id, Name: at.name, Arguments: args,
			},
			Partial: partial,
		}
	}

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
