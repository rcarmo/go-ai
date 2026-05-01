// Package openairesponses implements the OpenAI Responses API provider.
//
// This handles the newer OpenAI Responses API (used by GPT-5.x, o-series, Codex).
// Also serves as the base for Azure OpenAI Responses.
package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/eventstream"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiOpenAIResponses,
		Stream:       streamResponses,
		StreamSimple: streamResponsesSimple,
	})
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiAzureOpenAIResponses,
		Stream:       streamResponses,
		StreamSimple: streamResponsesSimple,
	})
}

func streamResponsesSimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamResponses(ctx, model, convCtx, opts)
}

func streamResponses(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "openai-responses", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("no API key for provider %s", model.Provider)}
			return
		}

		body := buildRequest(model, convCtx, opts)
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
			baseURL = goai.ResolveCloudflareBaseURL(model)
		}
		url := baseURL + "/responses"
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		if model.Provider == goai.ProviderCloudflareAIGateway {
			req.Header.Set("cf-aig-authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		req.Header.Set("Accept", "text/event-stream")
		if model.Api == goai.ApiAzureOpenAIResponses && opts != nil && opts.SessionID != "" {
			for k, v := range goai.AzureSessionHeaders(opts.SessionID) {
				req.Header.Set(k, v)
			}
		} else if opts != nil && opts.SessionID != "" {
			// Standard OpenAI: send session_id header if compat allows
			compat := getResponsesCompat(model)
			if compat.sendSessionIdHeader {
				req.Header.Set("session_id", opts.SessionID)
			}
		}

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

		processStream(resp.Body, model, ch)
	}()

	return ch
}

// --- Request ---

type responsesRequest struct {
	Model                string           `json:"model"`
	Input                json.RawMessage  `json:"input"`
	Stream               bool             `json:"stream"`
	Store                bool             `json:"store"`
	Tools                []toolDef        `json:"tools,omitempty"`
	Temperature          *float64         `json:"temperature,omitempty"`
	MaxOutputTokens      *int             `json:"max_output_tokens,omitempty"`
	Reasoning            *reasoningConfig `json:"reasoning,omitempty"`
	Include              []string         `json:"include,omitempty"`
	PromptCacheKey       string           `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string           `json:"prompt_cache_retention,omitempty"`
}

type reasoningConfig struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

type toolDef struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func buildRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) responsesRequest {
	req := responsesRequest{
		Model:  model.ID,
		Stream: true,
		Store:  false,
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		req.MaxOutputTokens = opts.MaxTokens
	}

	// Convert messages to Responses API input format
	input := convertMessages(model, convCtx)
	if model.Api == goai.ApiAzureOpenAIResponses {
		limited := goai.ApplyToolCallLimit(input, goai.DefaultToolCallLimitConfig())
		input = limited.Messages
	}
	inputJSON, _ := json.Marshal(input)
	req.Input = inputJSON

	// Convert tools
	for _, t := range convCtx.Tools {
		req.Tools = append(req.Tools, toolDef{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}

	// Reasoning — match pi-ai's format: {effort, summary} object + include encrypted content.
	if model.Reasoning {
		effort := "medium"
		if opts != nil && opts.Reasoning != nil {
			effort = string(*opts.Reasoning)
		}
		if model.Provider == goai.ProviderGitHubCopilot && effort == "" {
			// Copilot: omit reasoning block entirely if no effort requested,
			// matching pi-ai's behavior for github-copilot without explicit effort.
		} else {
			req.Reasoning = &reasoningConfig{Effort: effort, Summary: "auto"}
			req.Include = []string{"reasoning.encrypted_content"}
		}
	} else if opts != nil && opts.Reasoning != nil {
		// Non-reasoning model but explicit reasoning requested — pass through.
		req.Reasoning = &reasoningConfig{Effort: string(*opts.Reasoning), Summary: "auto"}
		req.Include = []string{"reasoning.encrypted_content"}
	}

	// Cache retention (compat-driven)
	compat := getResponsesCompat(model)
	if opts != nil && opts.SessionID != "" && opts.CacheRetention != goai.CacheRetentionNone {
		req.PromptCacheKey = opts.SessionID
	}
	if opts != nil && opts.CacheRetention == goai.CacheRetentionLong && compat.supportsLongCacheRetention {
		req.PromptCacheRetention = "24h"
	}

	return req
}

type responsesCompat struct {
	sendSessionIdHeader        bool
	supportsLongCacheRetention bool
}

func getResponsesCompat(model *goai.Model) responsesCompat {
	c := responsesCompat{
		sendSessionIdHeader:        true,
		supportsLongCacheRetention: true,
	}
	if goai.IsCloudflareProvider(model.Provider) {
		c.supportsLongCacheRetention = false
	}
	if model.ResponsesCompat != nil {
		if model.ResponsesCompat.SendSessionIdHeader != nil {
			c.sendSessionIdHeader = *model.ResponsesCompat.SendSessionIdHeader
		}
		if model.ResponsesCompat.SupportsLongCacheRetention != nil {
			c.supportsLongCacheRetention = *model.ResponsesCompat.SupportsLongCacheRetention
		}
	}
	return c
}

// convertMessages builds the Responses API input array.
func convertMessages(model *goai.Model, convCtx *goai.Context) []interface{} {
	var input []interface{}

	// System prompt
	if convCtx.SystemPrompt != "" {
		role := "developer"
		if !model.Reasoning {
			role = "system"
		}
		input = append(input, map[string]interface{}{
			"role":    role,
			"content": goai.SanitizeSurrogates(convCtx.SystemPrompt),
		})
	}

	transformed := goai.TransformMessages(convCtx.Messages, model)

	for _, msg := range transformed {
		switch msg.Role {
		case goai.RoleUser:
			content := buildUserContent(msg)
			if len(content) > 0 {
				input = append(input, map[string]interface{}{
					"role":    "user",
					"content": content,
				})
			}

		case goai.RoleAssistant:
			items := buildAssistantItems(msg, model)
			input = append(input, items...)

		case goai.RoleToolResult:
			textResult := extractText(msg.Content)
			callID := msg.ToolCallID
			if idx := strings.Index(callID, "|"); idx >= 0 {
				callID = callID[:idx]
			}
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  goai.SanitizeSurrogates(textResult),
			})
		}
	}

	return input
}

func buildUserContent(msg goai.Message) []map[string]interface{} {
	var content []map[string]interface{}
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			content = append(content, map[string]interface{}{
				"type": "input_text",
				"text": goai.SanitizeSurrogates(block.Text),
			})
		case "image":
			content = append(content, map[string]interface{}{
				"type":      "input_image",
				"detail":    "auto",
				"image_url": fmt.Sprintf("data:%s;base64,%s", block.MimeType, block.Data),
			})
		}
	}
	return content
}

func buildAssistantItems(msg goai.Message, model *goai.Model) []interface{} {
	// Check if this assistant message came from a different model variant.
	// When replaying cross-model messages, omit fc_ item IDs to avoid
	// OpenAI's reasoning/function-call pairing validation.
	isDifferentModel := msg.Model != "" && msg.Model != model.ID &&
		msg.Provider == model.Provider &&
		msg.Api == model.Api

	var items []interface{}
	for _, block := range msg.Content {
		switch block.Type {
		case "thinking":
			if block.ThinkingSignature != "" {
				// Replay the original reasoning item verbatim
				var item interface{}
				if json.Unmarshal([]byte(block.ThinkingSignature), &item) == nil {
					items = append(items, item)
				}
			}
		case "text":
			item := map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": []map[string]interface{}{{"type": "output_text", "text": goai.SanitizeSurrogates(block.Text)}},
				"status":  "completed",
			}
			// Include id from TextSignature for proper replay
			if block.TextSignature != "" {
				var sig struct {
					ID    string `json:"id"`
					Phase string `json:"phase"`
				}
				if json.Unmarshal([]byte(block.TextSignature), &sig) == nil && sig.ID != "" {
					msgID := sig.ID
					if len(msgID) > 64 {
						msgID = fmt.Sprintf("msg_%x", crc32Hash(msgID))
					}
					item["id"] = msgID
					if sig.Phase != "" {
						item["phase"] = sig.Phase
					}
				}
			}
			items = append(items, item)
		case "toolCall":
			callID := block.ID
			itemID := ""
			if idx := strings.Index(callID, "|"); idx >= 0 {
				itemID = callID[idx+1:]
				callID = callID[:idx]
			}
			// For different-model messages, omit fc_ item IDs to avoid
			// pairing validation between reasoning and function-call items.
			if isDifferentModel && strings.HasPrefix(itemID, "fc_") {
				itemID = ""
			}
			item := map[string]interface{}{
				"type":      "function_call",
				"call_id":   callID,
				"name":      block.Name,
				"arguments": mustJSON(block.Arguments),
			}
			if itemID != "" {
				item["id"] = itemID
			}
			items = append(items, item)
		}
	}
	return items
}

func extractText(blocks []goai.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func crc32Hash(s string) uint32 {
	return crc32.ChecksumIEEE([]byte(s))
}

// --- Stream processing ---

func processStream(body io.Reader, model *goai.Model, ch chan<- goai.Event) {
	partial := &goai.Message{
		Role:     goai.RoleAssistant,
		Api:      model.Api,
		Provider: model.Provider,
		Model:    model.ID,
		Usage:    &goai.Usage{},
	}

	ch <- &goai.StartEvent{Partial: partial}

	type activeItem struct {
		itemType    string // "reasoning", "message", "function_call"
		contentIdx  int
		partialJSON string
	}
	var current *activeItem

	events := eventstream.Parse(body)
	for sse := range events {
		if sse.Event == eventstream.EventError {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", sse.Data)}
			return
		}
		if sse.Data == "[DONE]" {
			break
		}

		data := []byte(sse.Data)
		if model.Api == goai.ApiAzureOpenAIResponses {
			var evt map[string]interface{}
			if json.Unmarshal(data, &evt) == nil {
				evt = goai.NormalizeAzureReasoningEvent(evt)
				if normalized, err := json.Marshal(evt); err == nil {
					data = normalized
				}
			}
		}

		var raw struct {
			Type     string          `json:"type"`
			Item     json.RawMessage `json:"item,omitempty"`
			Response json.RawMessage `json:"response,omitempty"`
			Delta    string          `json:"delta,omitempty"`
			Part     json.RawMessage `json:"part,omitempty"`
			Code     string          `json:"code,omitempty"`
			Message  string          `json:"message,omitempty"`
		}
		if json.Unmarshal(data, &raw) != nil {
			continue
		}

		switch raw.Type {
		case "response.created":
			var resp struct {
				ID string `json:"id"`
			}
			json.Unmarshal(raw.Response, &resp)
			partial.ResponseID = resp.ID

		case "response.output_item.added":
			var item struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				CallID string `json:"call_id"`
				Name   string `json:"name"`
				Args   string `json:"arguments"`
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

		case "response.refusal.delta":
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
				// Store the full item as thinkingSignature for replay
				partial.Content[idx].ThinkingSignature = string(raw.Item)
				ch <- &goai.ThinkingEndEvent{ContentIndex: idx, Content: partial.Content[idx].Thinking, Partial: partial}
			case "message":
				// Extract text signature from item
				var item struct {
					ID    string `json:"id"`
					Phase string `json:"phase,omitempty"`
				}
				json.Unmarshal(raw.Item, &item)
				sig := map[string]interface{}{"v": 1, "id": item.ID}
				if item.Phase != "" {
					sig["phase"] = item.Phase
				}
				sigJSON, _ := json.Marshal(sig)
				partial.Content[idx].TextSignature = string(sigJSON)
				ch <- &goai.TextEndEvent{ContentIndex: idx, Content: partial.Content[idx].Text, Partial: partial}
			case "function_call":
				args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
				if args == nil {
					args = map[string]interface{}{}
				}
				partial.Content[idx].Arguments = args
				ch <- &goai.ToolCallEndEvent{
					ContentIndex: idx,
					ToolCall: goai.ToolCall{
						Type:      "toolCall",
						ID:        partial.Content[idx].ID,
						Name:      partial.Content[idx].Name,
						Arguments: args,
					},
					Partial: partial,
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
					Input:       resp.Usage.InputTokens - cached,
					Output:      resp.Usage.OutputTokens,
					CacheRead:   cached,
					TotalTokens: resp.Usage.TotalTokens,
				}
				partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
			}

			partial.StopReason = mapStatus(resp.Status)
			// If we have tool calls and status is "stop", upgrade to "toolUse"
			for _, c := range partial.Content {
				if c.Type == "toolCall" && partial.StopReason == goai.StopReasonStop {
					partial.StopReason = goai.StopReasonToolUse
					break
				}
			}

		case "error":
			ch <- &goai.ErrorEvent{
				Reason: goai.StopReasonError,
				Err:    fmt.Errorf("API error %s: %s", raw.Code, raw.Message),
			}
			return

		case "response.failed":
			var resp struct {
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			json.Unmarshal(raw.Response, &resp)
			msg := "unknown error"
			if resp.Error != nil {
				msg = fmt.Sprintf("%s: %s", resp.Error.Code, resp.Error.Message)
			}
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("%s", msg)}
			return
		}
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}

	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

func mapStatus(status string) goai.StopReason {
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
