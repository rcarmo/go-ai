// Package anthropic implements the Anthropic Messages API provider.
package anthropic

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
)

const defaultBaseURL = "https://api.anthropic.com/v1"
const apiVersion = "2023-06-01"

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiAnthropicMessages,
		Stream:       streamAnthropic,
		StreamSimple: streamAnthropicSimple,
	})
}

func streamAnthropicSimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamAnthropic(ctx, model, convCtx, opts)
}

func streamAnthropic(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "anthropic-messages", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("no API key for Anthropic")}
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
		if baseURL == "" {
			baseURL = defaultBaseURL
		}

		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/messages", bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", apiKey)
		req.Header.Set("Anthropic-Version", apiVersion)
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

		processAnthropicStream(resp.Body, model, ch)
	}()

	return ch
}

// --- Request ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`

	// Image
	Source *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func buildRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) anthropicRequest {
	maxTokens := model.MaxTokens
	if opts != nil && opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	req := anthropicRequest{
		Model:     model.ID,
		MaxTokens: maxTokens,
		Stream:    true,
		System:    convCtx.SystemPrompt,
	}

	// Convert messages
	// Convert messages with cross-provider normalization
	transformed := goai.TransformMessages(convCtx.Messages, model)
	for _, m := range transformed {
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
				var blocks []anthropicContentBlock
				for _, b := range m.Content {
					switch b.Type {
					case "text":
						blocks = append(blocks, anthropicContentBlock{Type: "text", Text: goai.SanitizeSurrogates(b.Text)})
					case "image":
						blocks = append(blocks, anthropicContentBlock{
							Type: "image",
							Source: &anthropicImageSource{
								Type:      "base64",
								MediaType: b.MimeType,
								Data:      b.Data,
							},
						})
					}
				}
				req.Messages = append(req.Messages, anthropicMessage{Role: "user", Content: blocks})
			} else {
				req.Messages = append(req.Messages, anthropicMessage{
					Role:    "user",
					Content: goai.SanitizeSurrogates(extractText(m.Content)),
				})
			}
		case goai.RoleAssistant:
			var blocks []anthropicContentBlock
			for _, c := range m.Content {
				switch c.Type {
				case "text":
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: c.Text})
				case "toolCall":
					inputJSON, _ := json.Marshal(c.Arguments)
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    c.ID,
						Name:  c.Name,
						Input: inputJSON,
					})
				}
			}
			req.Messages = append(req.Messages, anthropicMessage{Role: "assistant", Content: blocks})
		case goai.RoleToolResult:
			req.Messages = append(req.Messages, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: m.ToolCallID,
					Content:   extractText(m.Content),
					IsError:   m.IsError,
				}},
			})
		}
	}

	// Convert tools
	for _, t := range convCtx.Tools {
		req.Tools = append(req.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	return req
}

func extractText(blocks []goai.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

// --- SSE processing ---

func processAnthropicStream(body io.Reader, model *goai.Model, ch chan<- goai.Event) {
	partial := &goai.Message{
		Role:     goai.RoleAssistant,
		Api:      model.Api,
		Provider: model.Provider,
		Model:    model.ID,
		Usage:    &goai.Usage{},
	}

	ch <- &goai.StartEvent{Partial: partial}

	events := eventstream.Parse(body)
	for sse := range events {
		switch sse.Event {
		case "content_block_start":
			var data struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id,omitempty"`
					Name string `json:"name,omitempty"`
				} `json:"content_block"`
			}
			if json.Unmarshal([]byte(sse.Data), &data) != nil {
				continue
			}
			switch data.ContentBlock.Type {
			case "text":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				ch <- &goai.TextStartEvent{ContentIndex: data.Index, Partial: partial}
			case "thinking":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
				ch <- &goai.ThinkingStartEvent{ContentIndex: data.Index, Partial: partial}
			case "tool_use":
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall",
					ID:   data.ContentBlock.ID,
					Name: data.ContentBlock.Name,
				})
				ch <- &goai.ToolCallStartEvent{ContentIndex: data.Index, Partial: partial}
			}

		case "content_block_delta":
			var data struct {
				Index int `json:"index"`
				Delta struct {
					Type            string `json:"type"`
					Text            string `json:"text,omitempty"`
					Thinking        string `json:"thinking,omitempty"`
					PartialJSON     string `json:"partial_json,omitempty"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(sse.Data), &data) != nil {
				continue
			}
			idx := data.Index
			if idx >= len(partial.Content) {
				continue
			}
			switch data.Delta.Type {
			case "text_delta":
				partial.Content[idx].Text += data.Delta.Text
				ch <- &goai.TextDeltaEvent{ContentIndex: idx, Delta: data.Delta.Text, Partial: partial}
			case "thinking_delta":
				partial.Content[idx].Thinking += data.Delta.Thinking
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: idx, Delta: data.Delta.Thinking, Partial: partial}
			case "input_json_delta":
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: idx, Delta: data.Delta.PartialJSON, Partial: partial}
			}

		case "content_block_stop":
			var data struct {
				Index int `json:"index"`
			}
			if json.Unmarshal([]byte(sse.Data), &data) != nil {
				continue
			}
			idx := data.Index
			if idx >= len(partial.Content) {
				continue
			}
			c := partial.Content[idx]
			switch c.Type {
			case "text":
				ch <- &goai.TextEndEvent{ContentIndex: idx, Content: c.Text, Partial: partial}
			case "thinking":
				ch <- &goai.ThinkingEndEvent{ContentIndex: idx, Content: c.Thinking, Partial: partial}
			case "toolCall":
				ch <- &goai.ToolCallEndEvent{
					ContentIndex: idx,
					ToolCall: goai.ToolCall{
						Type: "toolCall", ID: c.ID, Name: c.Name, Arguments: c.Arguments,
					},
					Partial: partial,
				}
			}

		case "message_delta":
			var data struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(sse.Data), &data) != nil {
				continue
			}
			partial.Usage.Output = data.Usage.OutputTokens
			partial.Usage.TotalTokens = partial.Usage.Input + partial.Usage.Output

			switch data.Delta.StopReason {
			case "end_turn":
				partial.StopReason = goai.StopReasonStop
			case "max_tokens":
				partial.StopReason = goai.StopReasonLength
			case "tool_use":
				partial.StopReason = goai.StopReasonToolUse
			}

		case "message_start":
			var data struct {
				Message struct {
					ID    string `json:"id"`
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						CacheRead    int `json:"cache_read_input_tokens"`
						CacheCreate  int `json:"cache_creation_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(sse.Data), &data) != nil {
				continue
			}
			partial.ResponseID = data.Message.ID
			partial.Usage.Input = data.Message.Usage.InputTokens
			partial.Usage.CacheRead = data.Message.Usage.CacheRead
			partial.Usage.CacheWrite = data.Message.Usage.CacheCreate

		case "message_stop":
			// Final
		}
	}

	partial.Timestamp = time.Now().UnixMilli()
	computeCosts(partial.Usage, model)

	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}

	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

func computeCosts(usage *goai.Usage, model *goai.Model) {
	m := 1_000_000.0
	usage.Cost.Input = float64(usage.Input) * model.Cost.Input / m
	usage.Cost.Output = float64(usage.Output) * model.Cost.Output / m
	usage.Cost.CacheRead = float64(usage.CacheRead) * model.Cost.CacheRead / m
	usage.Cost.CacheWrite = float64(usage.CacheWrite) * model.Cost.CacheWrite / m
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output + usage.Cost.CacheRead + usage.Cost.CacheWrite
}
