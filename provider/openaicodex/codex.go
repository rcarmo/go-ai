// Package openaicodex implements the OpenAI Codex Responses API provider.
//
// Uses WebSocket transport for streaming, falling back to SSE/HTTP.
// Requires OAuth authentication (ChatGPT Plus/Pro subscription).
package openaicodex

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
	"github.com/rcarmo/go-ai/internal/eventstream"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"nhooyr.io/websocket"
)

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiOpenAICodexResponses,
		Stream:       streamCodex,
		StreamSimple: streamCodexSimple,
	})
}

func streamCodexSimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamCodex(ctx, model, convCtx, opts)
}

func streamCodex(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)
		goai.GetLogger().Debug("stream start", "api", "openai-codex-responses", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("no API key for OpenAI Codex")}
			return
		}

		// Determine transport
		transport := goai.TransportAuto
		if opts != nil && opts.Transport != "" {
			transport = opts.Transport
		}

		if transport == goai.TransportWebSocket || transport == goai.TransportAuto {
			err := streamViaWebSocket(ctx, model, convCtx, opts, apiKey, ch)
			if err == nil {
				return
			}
			// Fall back to SSE if WebSocket fails and transport is auto
			if transport == goai.TransportAuto {
				goai.GetLogger().Debug("WebSocket fallback to SSE", "error", err)
			} else {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
				return
			}
		}

		// SSE fallback (same as OpenAI Responses but with Codex URL)
		streamViaSSE(ctx, model, convCtx, opts, apiKey, ch)
	}()

	return ch
}

func resolveCodexURL(baseURL string) string {
	if baseURL == "" {
		return "https://api.openai.com/v1/codex/responses"
	}
	normalized := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(normalized, "/codex") {
		return normalized + "/responses"
	}
	return normalized + "/codex/responses"
}

func resolveCodexWSURL(baseURL string) string {
	httpURL := resolveCodexURL(baseURL)
	httpURL = strings.Replace(httpURL, "https://", "wss://", 1)
	httpURL = strings.Replace(httpURL, "http://", "ws://", 1)
	return httpURL
}

// --- WebSocket transport ---

func streamViaWebSocket(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, apiKey string, ch chan<- goai.Event) error {
	wsURL := resolveCodexWSURL(model.BaseURL)
	goai.GetLogger().Debug("WebSocket connect", "url", wsURL, "provider", model.Provider)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	if opts != nil {
		for k, v := range opts.Headers {
			headers.Set(k, v)
		}
	}
	for k, v := range model.Headers {
		headers.Set(k, v)
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return fmt.Errorf("WebSocket dial: %w", err)
	}
	defer conn.CloseNow()

	// Build request body
	body := buildCodexRequest(model, convCtx, opts)
	payload, err := goai.InvokeOnPayload(opts, body, model)
	if err != nil {
		return err
	}
	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Send request over WebSocket
	if err := conn.Write(ctx, websocket.MessageText, bodyJSON); err != nil {
		return fmt.Errorf("WebSocket write: %w", err)
	}

	// Process responses
	partial := &goai.Message{
		Role:     goai.RoleAssistant,
		Api:      model.Api,
		Provider: model.Provider,
		Model:    model.ID,
		Usage:    &goai.Usage{},
	}

	ch <- &goai.StartEvent{Partial: partial}

	type activeItem struct {
		itemType    string
		contentIdx  int
		partialJSON string
	}
	var current *activeItem

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				goai.GetLogger().Debug("request aborted", "provider", model.Provider, "model", model.ID)
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()}
				return nil
			}
			// Normal close
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return nil
		}

		var raw struct {
			Type     string          `json:"type"`
			Item     json.RawMessage `json:"item,omitempty"`
			Response json.RawMessage `json:"response,omitempty"`
			Delta    string          `json:"delta,omitempty"`
			Code     string          `json:"code,omitempty"`
			Message  string          `json:"message,omitempty"`
		}
		if json.Unmarshal(data, &raw) != nil {
			continue
		}

		// Same event processing as OpenAI Responses
		switch raw.Type {
		case "response.created":
			var resp struct{ ID string `json:"id"` }
			json.Unmarshal(raw.Response, &resp)
			partial.ResponseID = resp.ID

		case "response.output_item.added":
			var item struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
			}
			json.Unmarshal(raw.Item, &item)

			switch item.Type {
			case "reasoning":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
				idx := len(partial.Content) - 1
				current = &activeItem{itemType: "reasoning", contentIdx: idx}
				ch <- &goai.ThinkingStartEvent{ContentIndex: idx, Partial: partial}
			case "message":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				idx := len(partial.Content) - 1
				current = &activeItem{itemType: "message", contentIdx: idx}
				ch <- &goai.TextStartEvent{ContentIndex: idx, Partial: partial}
			case "function_call":
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall",
					ID:   fmt.Sprintf("%s|%s", item.CallID, item.ID),
					Name: item.Name,
				})
				idx := len(partial.Content) - 1
				current = &activeItem{itemType: "function_call", contentIdx: idx}
				ch <- &goai.ToolCallStartEvent{ContentIndex: idx, Partial: partial}
			}

		case "response.reasoning_summary_text.delta":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += raw.Delta
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.output_text.delta":
			if current != nil && current.itemType == "message" {
				partial.Content[current.contentIdx].Text += raw.Delta
				ch <- &goai.TextDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.function_call_arguments.delta":
			if current != nil && current.itemType == "function_call" {
				current.partialJSON += raw.Delta
				args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
				if args != nil {
					partial.Content[current.contentIdx].Arguments = args
				}
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.output_item.done":
			if current == nil {
				continue
			}
			idx := current.contentIdx
			switch current.itemType {
			case "reasoning":
				partial.Content[idx].ThinkingSignature = string(raw.Item)
				ch <- &goai.ThinkingEndEvent{ContentIndex: idx, Content: partial.Content[idx].Thinking, Partial: partial}
			case "message":
				ch <- &goai.TextEndEvent{ContentIndex: idx, Content: partial.Content[idx].Text, Partial: partial}
			case "function_call":
				args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
				if args == nil {
					args = map[string]interface{}{}
				}
				partial.Content[idx].Arguments = args
				ch <- &goai.ToolCallEndEvent{
					ContentIndex: idx,
					ToolCall:     goai.ToolCall{Type: "toolCall", ID: partial.Content[idx].ID, Name: partial.Content[idx].Name, Arguments: args},
					Partial:      partial,
				}
			}
			current = nil

		case "response.completed":
			var resp struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Usage  *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
					TotalTokens  int `json:"total_tokens"`
					InputDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			}
			json.Unmarshal(raw.Response, &resp)
			if resp.ID != "" {
				partial.ResponseID = resp.ID
			}
			if resp.Usage != nil {
				cached := 0
				if resp.Usage.InputDetails != nil {
					cached = resp.Usage.InputDetails.CachedTokens
				}
				partial.Usage = &goai.Usage{
					Input: resp.Usage.InputTokens - cached, Output: resp.Usage.OutputTokens,
					CacheRead: cached, TotalTokens: resp.Usage.TotalTokens,
				}
				partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
			}
			partial.StopReason = mapCodexStatus(resp.Status)
			for _, c := range partial.Content {
				if c.Type == "toolCall" && partial.StopReason == goai.StopReasonStop {
					partial.StopReason = goai.StopReasonToolUse
					break
				}
			}

		case "error":
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("API error %s: %s", raw.Code, raw.Message)}
			return nil

		case "response.failed":
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("response failed")}
			return nil
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}
	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
	return nil
}

// --- SSE fallback ---

func streamViaSSE(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, apiKey string, ch chan<- goai.Event) {
	body := buildCodexRequest(model, convCtx, opts)
	payload, err := goai.InvokeOnPayload(opts, body, model)
	if err != nil {
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
		return
	}
	bodyJSON, _ := json.Marshal(payload)

	url := resolveCodexURL(model.BaseURL)
	goai.GetLogger().Debug("HTTP request", "url", url, "provider", model.Provider, "model", model.ID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
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
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()}
		} else {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
		}
		return
	}
	defer resp.Body.Close()

	goai.InvokeOnResponse(opts, resp, model)

	if resp.StatusCode != 200 {
		goai.GetLogger().Warn("HTTP error response", "status", resp.StatusCode, "provider", model.Provider, "model", model.ID)
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))}
		return
	}

	// Reuse same SSE processing as OpenAI Responses
	processCodexSSE(resp.Body, model, ch)
}

// processCodexSSE is identical to the Responses API SSE processing
func processCodexSSE(body io.Reader, model *goai.Model, ch chan<- goai.Event) {
	partial := &goai.Message{
		Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{},
	}
	ch <- &goai.StartEvent{Partial: partial}

	type activeItem struct {
		itemType    string
		contentIdx  int
		partialJSON string
	}
	var current *activeItem

	events := eventstream.Parse(body)
	for sse := range events {
		if sse.Data == "[DONE]" {
			break
		}
		var raw struct {
			Type     string          `json:"type"`
			Item     json.RawMessage `json:"item,omitempty"`
			Response json.RawMessage `json:"response,omitempty"`
			Delta    string          `json:"delta,omitempty"`
		}
		if json.Unmarshal([]byte(sse.Data), &raw) != nil {
			continue
		}

		switch raw.Type {
		case "response.output_text.delta":
			if current != nil && current.itemType == "message" {
				partial.Content[current.contentIdx].Text += raw.Delta
				ch <- &goai.TextDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}
		case "response.output_item.added":
			var item struct{ Type, ID, CallID, Name string }
			json.Unmarshal(raw.Item, &item)
			switch item.Type {
			case "message":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				current = &activeItem{itemType: "message", contentIdx: len(partial.Content) - 1}
				ch <- &goai.TextStartEvent{ContentIndex: current.contentIdx, Partial: partial}
			case "function_call":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "toolCall", ID: fmt.Sprintf("%s|%s", item.CallID, item.ID), Name: item.Name})
				current = &activeItem{itemType: "function_call", contentIdx: len(partial.Content) - 1}
				ch <- &goai.ToolCallStartEvent{ContentIndex: current.contentIdx, Partial: partial}
			}
		case "response.completed":
			var resp struct {
				Status string `json:"status"`
				Usage  *struct {
					InputTokens, OutputTokens, TotalTokens int
				} `json:"usage"`
			}
			json.Unmarshal(raw.Response, &resp)
			if resp.Usage != nil {
				partial.Usage = &goai.Usage{Input: resp.Usage.InputTokens, Output: resp.Usage.OutputTokens, TotalTokens: resp.Usage.TotalTokens}
				partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
			}
			partial.StopReason = mapCodexStatus(resp.Status)
		}
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}
	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

// --- Request building ---

type codexRequest struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Stream          bool            `json:"stream"`
	Tools           []codexTool     `json:"tools,omitempty"`
	MaxOutputTokens *int            `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	Reasoning       interface{}     `json:"reasoning,omitempty"`
}

type codexTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func buildCodexRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) codexRequest {
	req := codexRequest{
		Model:  model.ID,
		Stream: true,
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		req.MaxOutputTokens = opts.MaxTokens
		if opts.Reasoning != nil {
			req.Reasoning = map[string]interface{}{"effort": string(*opts.Reasoning)}
		}
	}

	// Build input (same format as OpenAI Responses)
	input := buildCodexInput(model, convCtx)
	inputJSON, _ := json.Marshal(input)
	req.Input = inputJSON

	for _, t := range convCtx.Tools {
		req.Tools = append(req.Tools, codexTool{
			Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Parameters,
		})
	}

	return req
}

func buildCodexInput(model *goai.Model, convCtx *goai.Context) []interface{} {
	var input []interface{}
	if convCtx.SystemPrompt != "" {
		input = append(input, map[string]interface{}{
			"role": "developer", "content": goai.SanitizeSurrogates(convCtx.SystemPrompt),
		})
	}
	transformed := goai.TransformMessages(convCtx.Messages, model)
	for _, msg := range transformed {
		switch msg.Role {
		case goai.RoleUser:
			var content []map[string]interface{}
			for _, b := range msg.Content {
				if b.Type == "text" {
					content = append(content, map[string]interface{}{"type": "input_text", "text": goai.SanitizeSurrogates(b.Text)})
				}
			}
			if len(content) > 0 {
				input = append(input, map[string]interface{}{"role": "user", "content": content})
			}
		case goai.RoleAssistant:
			for _, b := range msg.Content {
				if b.Type == "text" {
					input = append(input, map[string]interface{}{
						"type": "message", "role": "assistant", "status": "completed",
						"content": []map[string]interface{}{{"type": "output_text", "text": goai.SanitizeSurrogates(b.Text)}},
					})
				} else if b.Type == "toolCall" {
					callID := b.ID
					itemID := ""
					if idx := strings.Index(callID, "|"); idx >= 0 {
						itemID = callID[idx+1:]
						callID = callID[:idx]
					}
					item := map[string]interface{}{
						"type": "function_call", "call_id": callID, "name": b.Name,
						"arguments": func() string { j, _ := json.Marshal(b.Arguments); return string(j) }(),
					}
					if itemID != "" {
						item["id"] = itemID
					}
					input = append(input, item)
				}
			}
		case goai.RoleToolResult:
			text := ""
			for _, b := range msg.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			callID := msg.ToolCallID
			if idx := strings.Index(callID, "|"); idx >= 0 {
				callID = callID[:idx]
			}
			input = append(input, map[string]interface{}{
				"type": "function_call_output", "call_id": callID, "output": goai.SanitizeSurrogates(text),
			})
		}
	}
	return input
}

func mapCodexStatus(status string) goai.StopReason {
	switch status {
	case "completed":
		return goai.StopReasonStop
	case "incomplete":
		return goai.StopReasonLength
	case "failed", "cancelled":
		return goai.StopReasonError
	default:
		return goai.StopReasonStop
	}
}
