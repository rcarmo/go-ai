// Package geminicli implements the Google Gemini CLI / Cloud Code Assist provider.
//
// Uses the Cloud Code Assist API (v1internal:streamGenerateContent) with OAuth.
// Shared with the Antigravity provider (same wire format, different endpoints).
// The API key is expected to be JSON: {"token":"...","projectId":"..."}
package geminicli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/eventstream"
)

const defaultEndpoint = "https://cloudcode-pa.googleapis.com"

var toolCallCounter int64

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiGoogleGeminiCLI,
		Stream:       streamGeminiCLI,
		StreamSimple: streamGeminiCLISimple,
	})
}

func streamGeminiCLISimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamGeminiCLI(ctx, model, convCtx, opts)
}

func streamGeminiCLI(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		// Parse API key — expected format: {"token":"...","projectId":"..."}
		apiKeyRaw := goai.ResolveAPIKey(model, opts)
		if apiKeyRaw == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError,
				Err: fmt.Errorf("Google Cloud Code Assist requires OAuth authentication")}
			return
		}

		var creds struct {
			Token     string `json:"token"`
			ProjectID string `json:"projectId"`
		}
		if err := json.Unmarshal([]byte(apiKeyRaw), &creds); err != nil || creds.Token == "" || creds.ProjectID == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError,
				Err: fmt.Errorf("invalid Google Cloud credentials (expected {token,projectId} JSON)")}
			return
		}

		body := buildRequest(model, convCtx, creds.ProjectID, opts)
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		endpoint := defaultEndpoint
		if model.BaseURL != "" {
			endpoint = strings.TrimRight(model.BaseURL, "/")
		}
		url := endpoint + "/v1internal:streamGenerateContent?alt=sse"

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+creds.Token)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("User-Agent", "cloud-code-assist/1.0")
		req.Header.Set("X-Goog-Api-Client", "cl/head cloud-code-assist/1.0")

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

		if resp.StatusCode != 200 {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			ch <- &goai.ErrorEvent{
				Reason: goai.StopReasonError,
				Err:    fmt.Errorf("Cloud Code Assist API error (%d): %s", resp.StatusCode, string(bodyBytes)),
			}
			return
		}

		processStream(resp.Body, model, ch)
	}()

	return ch
}

// --- Request types ---

type ccaRequest struct {
	Project     string     `json:"project"`
	Model       string     `json:"model"`
	Request     ccaInner   `json:"request"`
	RequestType string     `json:"requestType,omitempty"`
	UserAgent   string     `json:"userAgent,omitempty"`
}

type ccaInner struct {
	Contents          []geminiContent         `json:"contents"`
	SessionID         string                  `json:"sessionId,omitempty"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiToolDecl        `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig       `json:"toolConfig,omitempty"`
}

// Reuse Gemini content types
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	Thought          *bool               `json:"thought,omitempty"`
	ThoughtSignature string              `json:"thoughtSignature,omitempty"`
	InlineData       *geminiInlineData   `json:"inlineData,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type geminiFuncResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type geminiGenerationConfig struct {
	Temperature     *float64              `json:"temperature,omitempty"`
	MaxOutputTokens *int                  `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	IncludeThoughts *bool  `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type geminiToolDecl struct {
	FunctionDeclarations []geminiToolFunc `json:"functionDeclarations"`
}

type geminiToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig *geminiFuncCallingConfig `json:"functionCallingConfig,omitempty"`
}

type geminiFuncCallingConfig struct {
	Mode string `json:"mode"`
}

func buildRequest(model *goai.Model, convCtx *goai.Context, projectID string, opts *goai.StreamOptions) ccaRequest {
	inner := ccaInner{}

	// System prompt
	if convCtx.SystemPrompt != "" {
		inner.SystemInstruction = &geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: goai.SanitizeSurrogates(convCtx.SystemPrompt)}},
		}
	}

	// Session ID for caching
	if opts != nil && opts.SessionID != "" {
		inner.SessionID = opts.SessionID
	}

	// Messages
	inner.Contents = convertMessages(model, convCtx)

	// Generation config
	genConfig := &geminiGenerationConfig{}
	hasConfig := false
	if opts != nil && opts.Temperature != nil {
		genConfig.Temperature = opts.Temperature
		hasConfig = true
	}
	if opts != nil && opts.MaxTokens != nil {
		genConfig.MaxOutputTokens = opts.MaxTokens
		hasConfig = true
	}
	if model.Reasoning && opts != nil && opts.Reasoning != nil {
		tc := &geminiThinkingConfig{}
		t := true
		tc.IncludeThoughts = &t
		tc.ThinkingLevel = strings.ToUpper(string(goai.ClampReasoning(*opts.Reasoning)))
		genConfig.ThinkingConfig = tc
		hasConfig = true
	}
	if hasConfig {
		inner.GenerationConfig = genConfig
	}

	// Tools — use legacy parameters field for Claude models on CCA
	if len(convCtx.Tools) > 0 {
		var funcs []geminiToolFunc
		for _, t := range convCtx.Tools {
			funcs = append(funcs, geminiToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
		inner.Tools = []geminiToolDecl{{FunctionDeclarations: funcs}}
	}

	return ccaRequest{
		Project: projectID,
		Model:   model.ID,
		Request: inner,
	}
}

func convertMessages(model *goai.Model, convCtx *goai.Context) []geminiContent {
	var contents []geminiContent
	transformed := goai.TransformMessages(convCtx.Messages, model)

	for _, msg := range transformed {
		switch msg.Role {
		case goai.RoleUser:
			var parts []geminiPart
			for _, b := range msg.Content {
				switch b.Type {
				case "text":
					parts = append(parts, geminiPart{Text: goai.SanitizeSurrogates(b.Text)})
				case "image":
					parts = append(parts, geminiPart{
						InlineData: &geminiInlineData{MimeType: b.MimeType, Data: b.Data},
					})
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "user", Parts: parts})
			}

		case goai.RoleAssistant:
			var parts []geminiPart
			for _, b := range msg.Content {
				switch b.Type {
				case "text":
					if b.Text != "" {
						parts = append(parts, geminiPart{Text: goai.SanitizeSurrogates(b.Text)})
					}
				case "thinking":
					if b.Thinking != "" {
						t := true
						parts = append(parts, geminiPart{Thought: &t, Text: goai.SanitizeSurrogates(b.Thinking)})
					}
				case "toolCall":
					parts = append(parts, geminiPart{
						FunctionCall: &geminiFunctionCall{Name: b.Name, Args: b.Arguments},
					})
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "model", Parts: parts})
			}

		case goai.RoleToolResult:
			text := ""
			for _, b := range msg.Content {
				if b.Type == "text" {
					text += b.Text
				}
			}
			resp := map[string]interface{}{}
			if msg.IsError {
				resp["error"] = goai.SanitizeSurrogates(text)
			} else {
				resp["output"] = goai.SanitizeSurrogates(text)
			}
			part := geminiPart{
				FunctionResponse: &geminiFuncResponse{Name: msg.ToolName, Response: resp},
			}
			if len(contents) > 0 {
				last := &contents[len(contents)-1]
				if last.Role == "user" && len(last.Parts) > 0 && last.Parts[0].FunctionResponse != nil {
					last.Parts = append(last.Parts, part)
					continue
				}
			}
			contents = append(contents, geminiContent{Role: "user", Parts: []geminiPart{part}})
		}
	}

	return contents
}

// --- Stream processing ---
// CCA wraps Gemini response in { response: { candidates, usageMetadata } }

type ccaStreamChunk struct {
	Response *struct {
		Candidates    []ccaCandidate `json:"candidates,omitempty"`
		UsageMetadata *ccaUsage      `json:"usageMetadata,omitempty"`
		ResponseID    string         `json:"responseId,omitempty"`
	} `json:"response,omitempty"`
}

type ccaCandidate struct {
	Content      *geminiContent `json:"content,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
}

type ccaUsage struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
}

func processStream(body io.Reader, model *goai.Model, ch chan<- goai.Event) {
	partial := &goai.Message{
		Role:     goai.RoleAssistant,
		Api:      model.Api,
		Provider: model.Provider,
		Model:    model.ID,
		Usage:    &goai.Usage{},
	}

	ch <- &goai.StartEvent{Partial: partial}

	type currentBlock struct {
		blockType string
		index     int
	}
	var current *currentBlock

	events := eventstream.Parse(body)
	for sse := range events {
		if sse.Data == "[DONE]" || sse.Data == "" {
			continue
		}

		var chunk ccaStreamChunk
		if json.Unmarshal([]byte(sse.Data), &chunk) != nil {
			continue
		}

		resp := chunk.Response
		if resp == nil {
			continue
		}

		if resp.ResponseID != "" {
			partial.ResponseID = resp.ResponseID
		}

		if len(resp.Candidates) > 0 {
			cand := resp.Candidates[0]
			if cand.Content != nil {
				for _, part := range cand.Content.Parts {
					isThinking := part.Thought != nil && *part.Thought

					if part.Text != "" {
						if current == nil ||
							(isThinking && current.blockType != "thinking") ||
							(!isThinking && current.blockType != "text") {
							if current != nil {
								switch current.blockType {
					case "text":
						ch <- &goai.TextEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Text, Partial: partial}
					case "thinking":
						ch <- &goai.ThinkingEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Thinking, Partial: partial}
					}
							}
							if isThinking {
								partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
								idx := len(partial.Content) - 1
								current = &currentBlock{"thinking", idx}
								ch <- &goai.ThinkingStartEvent{ContentIndex: idx, Partial: partial}
							} else {
								partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
								idx := len(partial.Content) - 1
								current = &currentBlock{"text", idx}
								ch <- &goai.TextStartEvent{ContentIndex: idx, Partial: partial}
							}
						}

						if isThinking {
							partial.Content[current.index].Thinking += part.Text
							ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.index, Delta: part.Text, Partial: partial}
						} else {
							partial.Content[current.index].Text += part.Text
							ch <- &goai.TextDeltaEvent{ContentIndex: current.index, Delta: part.Text, Partial: partial}
						}
					}

					if part.FunctionCall != nil {
						if current != nil {
							switch current.blockType {
					case "text":
						ch <- &goai.TextEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Text, Partial: partial}
					case "thinking":
						ch <- &goai.ThinkingEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Thinking, Partial: partial}
					}
							current = nil
						}
						tcID := fmt.Sprintf("%s_%d_%d", part.FunctionCall.Name, time.Now().UnixMilli(), atomic.AddInt64(&toolCallCounter, 1))
						tc := goai.ContentBlock{
							Type: "toolCall", ID: tcID, Name: part.FunctionCall.Name, Arguments: part.FunctionCall.Args,
						}
						partial.Content = append(partial.Content, tc)
						idx := len(partial.Content) - 1
						argsJSON, _ := json.Marshal(tc.Arguments)
						ch <- &goai.ToolCallStartEvent{ContentIndex: idx, Partial: partial}
						ch <- &goai.ToolCallDeltaEvent{ContentIndex: idx, Delta: string(argsJSON), Partial: partial}
						ch <- &goai.ToolCallEndEvent{
							ContentIndex: idx,
							ToolCall:     goai.ToolCall{Type: "toolCall", ID: tcID, Name: tc.Name, Arguments: tc.Arguments},
							Partial:      partial,
						}
					}
				}
			}

			if cand.FinishReason != "" {
				partial.StopReason = mapFinishReason(cand.FinishReason)
				for _, c := range partial.Content {
					if c.Type == "toolCall" {
						partial.StopReason = goai.StopReasonToolUse
						break
					}
				}
			}
		}

		if resp.UsageMetadata != nil {
			u := resp.UsageMetadata
			partial.Usage = &goai.Usage{
				Input:       u.PromptTokenCount - u.CachedContentTokenCount,
				Output:      u.CandidatesTokenCount + u.ThoughtsTokenCount,
				CacheRead:   u.CachedContentTokenCount,
				TotalTokens: u.TotalTokenCount,
			}
			partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
		}
	}

	if current != nil {
		switch current.blockType {
					case "text":
						ch <- &goai.TextEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Text, Partial: partial}
					case "thinking":
						ch <- &goai.ThinkingEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Thinking, Partial: partial}
					}
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}
	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}


func mapFinishReason(reason string) goai.StopReason {
	switch reason {
	case "STOP":
		return goai.StopReasonStop
	case "MAX_TOKENS":
		return goai.StopReasonLength
	default:
		return goai.StopReasonError
	}
}
