// Package google implements the Google Generative AI (Gemini) provider.
//
// Supports Gemini models via the REST API with streaming, thinking/reasoning,
// tool calling with thought signatures, and multi-turn conversations.
// Also serves as the base for Google Vertex AI (same wire format, different auth).
package google

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/sse"
)

var toolCallCounter int64

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiGoogleGenerativeAI,
		Stream:       streamGoogle,
		StreamSimple: streamGoogleSimple,
	})
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiGoogleVertex,
		Stream:       streamGoogle,
		StreamSimple: streamGoogleSimple,
	})
}

func streamGoogleSimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamGoogle(ctx, model, convCtx, opts)
}

func streamGoogle(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "google-generative-ai", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		if apiKey == "" {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("no API key for provider: %s", model.Provider)}
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

		// Build URL: REST API for Gemini/Vertex.
		url, err := buildStreamURL(model, apiKey, opts)
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
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

		processStream(resp.Body, model, ch)
	}()

	return ch
}

func buildStreamURL(model *goai.Model, apiKey string, opts *goai.StreamOptions) (string, error) {
	baseURL := model.BaseURL
	if model.Api == goai.ApiGoogleVertex {
		project, location, err := resolveVertexProjectLocation(opts)
		if err != nil {
			return "", err
		}
		if baseURL == "" {
			baseURL = "https://{location}-aiplatform.googleapis.com"
		}
		baseURL = strings.ReplaceAll(baseURL, "{location}", location)
		endpoint := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:streamGenerateContent?alt=sse",
			strings.TrimRight(baseURL, "/"), url.PathEscape(project), url.PathEscape(location), url.PathEscape(model.ID))
		if apiKey != "" && !strings.HasPrefix(apiKey, "<") {
			endpoint += "&key=" + url.QueryEscape(apiKey)
		}
		return endpoint, nil
	}
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	// REST streaming endpoint. Escape path/query components so unusual model
	// aliases or API keys containing reserved characters cannot corrupt the URL.
	return fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", strings.TrimRight(baseURL, "/"), url.PathEscape(model.ID), url.QueryEscape(apiKey)), nil
}

func resolveVertexProjectLocation(opts *goai.StreamOptions) (string, string, error) {
	env := goai.ProviderEnvFromOptions(opts)
	project := ""
	location := ""
	if opts != nil {
		project = opts.Project
		location = opts.Location
	}
	if project == "" {
		project = goai.GetProviderEnvValue("GOOGLE_CLOUD_PROJECT", env)
	}
	if project == "" {
		project = goai.GetProviderEnvValue("GCLOUD_PROJECT", env)
	}
	if location == "" {
		location = goai.GetProviderEnvValue("GOOGLE_CLOUD_LOCATION", env)
	}
	if project == "" {
		return "", "", fmt.Errorf("vertex AI requires a project ID; set GOOGLE_CLOUD_PROJECT/GCLOUD_PROJECT or pass Project in options")
	}
	if location == "" {
		return "", "", fmt.Errorf("vertex AI requires a location; set GOOGLE_CLOUD_LOCATION or pass Location in options")
	}
	return project, location, nil
}

// --- Request types ---

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiToolDecl        `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig       `json:"toolConfig,omitempty"`
}

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
	ID   string                 `json:"id,omitempty"`
}

type geminiFuncResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
	ID       string                 `json:"id,omitempty"`
	Parts    []geminiPart           `json:"parts,omitempty"`
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
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	ParametersJsonSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig *geminiFuncCallingConfig `json:"functionCallingConfig,omitempty"`
}

type geminiFuncCallingConfig struct {
	Mode string `json:"mode"` // AUTO, NONE, ANY
}

func buildRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) geminiRequest {
	req := geminiRequest{}

	// System prompt
	if convCtx.SystemPrompt != "" {
		req.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: goai.SanitizeSurrogates(convCtx.SystemPrompt)}},
		}
	}

	// Convert messages
	req.Contents = convertMessages(model, convCtx)

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

	// Thinking configuration — mirrors upstream's model-specific dispatch.
	if model.Reasoning {
		reasoningRequested := opts != nil && opts.Reasoning != nil && *opts.Reasoning != ""
		if reasoningRequested {
			level, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning))
			if !ok || level == "off" {
				level = string(goai.ThinkingHigh)
			}
			tc := &geminiThinkingConfig{}
			t := true
			tc.IncludeThoughts = &t
			if isGemini3ProModel(model) || isGemini3FlashModel(model) || isGemma4Model(model) {
				tc.ThinkingLevel = getGoogleThinkingLevel(level, model)
			} else {
				budget := getGoogleBudget(model, level)
				tc.ThinkingBudget = &budget
			}
			genConfig.ThinkingConfig = tc
			hasConfig = true
		} else {
			// Disable thinking
			tc := getDisabledThinkingConfig(model)
			genConfig.ThinkingConfig = tc
			hasConfig = true
		}
	}

	if hasConfig {
		req.GenerationConfig = genConfig
	}

	// Tools
	if len(convCtx.Tools) > 0 {
		var funcs []geminiToolFunc
		for _, t := range convCtx.Tools {
			funcs = append(funcs, geminiToolFunc{
				Name:                 t.Name,
				Description:          t.Description,
				ParametersJsonSchema: t.Parameters,
			})
		}
		req.Tools = []geminiToolDecl{{FunctionDeclarations: funcs}}
	}

	return req
}

func convertMessages(model *goai.Model, convCtx *goai.Context) []geminiContent {
	var contents []geminiContent
	transformed := goai.TransformMessages(convCtx.Messages, model)

	for _, msg := range transformed {
		switch msg.Role {
		case goai.RoleUser:
			var parts []geminiPart
			for _, block := range msg.Content {
				switch block.Type {
				case "text":
					parts = append(parts, geminiPart{Text: goai.SanitizeSurrogates(block.Text)})
				case "image":
					parts = append(parts, geminiPart{
						InlineData: &geminiInlineData{MimeType: block.MimeType, Data: block.Data},
					})
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "user", Parts: parts})
			}

		case goai.RoleAssistant:
			var parts []geminiPart
			isSameModel := msg.Provider == model.Provider && msg.Model == model.ID
			for _, block := range msg.Content {
				switch block.Type {
				case "text":
					if block.Text == "" {
						continue
					}
					p := geminiPart{Text: goai.SanitizeSurrogates(block.Text)}
					if isSameModel && isValidBase64Signature(block.TextSignature) {
						p.ThoughtSignature = block.TextSignature
					}
					parts = append(parts, p)
				case "thinking":
					if block.Thinking == "" {
						continue
					}
					if isSameModel {
						t := true
						p := geminiPart{Thought: &t, Text: goai.SanitizeSurrogates(block.Thinking)}
						if isValidBase64Signature(block.ThinkingSignature) {
							p.ThoughtSignature = block.ThinkingSignature
						}
						parts = append(parts, p)
					} else {
						parts = append(parts, geminiPart{Text: goai.SanitizeSurrogates(block.Thinking)})
					}
				case "toolCall":
					p := geminiPart{
						FunctionCall: &geminiFunctionCall{
							Name: block.Name,
							Args: block.Arguments,
						},
					}
					if requiresToolCallID(model.ID) {
						p.FunctionCall.ID = normalizeGoogleToolCallID(block.ID)
					}
					if isSameModel && isValidBase64Signature(block.ThoughtSignature) {
						p.ThoughtSignature = block.ThoughtSignature
					}
					parts = append(parts, p)
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "model", Parts: parts})
			}

		case goai.RoleToolResult:
			textResult := ""
			var imageParts []geminiPart
			for _, b := range msg.Content {
				if b.Type == "text" {
					textResult += b.Text
				} else if b.Type == "image" && b.MimeType != "" && b.Data != "" {
					imageParts = append(imageParts, geminiPart{
						InlineData: &geminiInlineData{MimeType: b.MimeType, Data: b.Data},
					})
				}
			}
			hasImages := len(imageParts) > 0
			resp := map[string]interface{}{}
			if msg.IsError {
				resp["error"] = goai.SanitizeSurrogates(textResult)
			} else if textResult != "" {
				resp["output"] = goai.SanitizeSurrogates(textResult)
			} else if hasImages {
				resp["output"] = "(see attached image)"
			}

			part := geminiPart{
				FunctionResponse: &geminiFuncResponse{
					Name:     msg.ToolName,
					Response: resp,
				},
			}
			if hasImages && supportsMultimodalFunctionResponse(model.ID) {
				part.FunctionResponse.Parts = imageParts
			}
			if requiresToolCallID(model.ID) {
				part.FunctionResponse.ID = normalizeGoogleToolCallID(msg.ToolCallID)
			}

			// Merge tool results into existing user turn if present
			if len(contents) > 0 {
				last := &contents[len(contents)-1]
				if last.Role == "user" && len(last.Parts) > 0 && last.Parts[0].FunctionResponse != nil {
					last.Parts = append(last.Parts, part)
					continue
				}
			}
			contents = append(contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{part},
			})
		}
	}

	return contents
}

// requiresToolCallID returns true for non-Gemini models (Claude, GPT-OSS) served via
// the Gemini API that require explicit tool call IDs.
func requiresToolCallID(modelID string) bool {
	return strings.HasPrefix(modelID, "claude-") || strings.HasPrefix(modelID, "gpt-oss-")
}

// supportsMultimodalFunctionResponse returns true for Gemini 3+ models which
// support image parts directly in functionResponse.
func supportsMultimodalFunctionResponse(modelID string) bool {
	v := getGeminiMajorVersion(modelID)
	if v > 0 {
		return v >= 3
	}
	// Non-Gemini models (claude, gpt-oss) default to supporting it
	return true
}

// getGeminiMajorVersion extracts the major version from a Gemini model ID.
func getGeminiMajorVersion(modelID string) int {
	lower := strings.ToLower(modelID)
	// Match patterns like gemini-3, gemini-live-3, etc.
	var prefix string
	if strings.HasPrefix(lower, "gemini-live-") {
		prefix = lower[len("gemini-live-"):]
	} else if strings.HasPrefix(lower, "gemini-") {
		prefix = lower[len("gemini-"):]
	} else {
		return 0
	}
	// Extract leading digits
	n := 0
	for _, c := range prefix {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	return n
}

// normalizeGoogleToolCallID sanitizes a tool call ID for the Google API.
func normalizeGoogleToolCallID(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func isValidBase64Signature(sig string) bool {
	if sig == "" || len(sig)%4 != 0 {
		return false
	}
	for _, c := range sig {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}

// --- Stream processing ---

// Gemini SSE stream returns JSON chunks with candidates[].content.parts[]
type geminiStreamChunk struct {
	Candidates    []geminiCandidate `json:"candidates,omitempty"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
	ResponseID    string            `json:"responseId,omitempty"`
}

type geminiCandidate struct {
	Content      *geminiContent `json:"content,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
}

type geminiUsage struct {
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

	type currentBlock = currentBlockT
	var current *currentBlock

	// Gemini REST streaming with alt=sse returns Server-Sent Events. Use the
	// shared parser so multi-line events and long chunks are handled correctly
	// and incrementally instead of buffering the full response body.
	for evt := range sse.Parse(body) {
		if evt.Event == sse.EventError {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", evt.Data)}
			return
		}
		if evt.Data == "" || evt.Data == "[DONE]" {
			continue
		}

		var chunk geminiStreamChunk
		if json.Unmarshal([]byte(evt.Data), &chunk) != nil {
			continue
		}

		// Update response ID
		if chunk.ResponseID != "" {
			partial.ResponseID = chunk.ResponseID
		}

		// Process candidates
		if len(chunk.Candidates) > 0 {
			cand := chunk.Candidates[0]
			if cand.Content != nil {
				for _, part := range cand.Content.Parts {
					isThinking := part.Thought != nil && *part.Thought

					// Text or thinking content
					if part.Text != "" {
						if current == nil ||
							(isThinking && current.blockType != "thinking") ||
							(!isThinking && current.blockType != "text") {
							// Close previous block
							if current != nil {
								closeBlock(current, partial, ch)
							}
							// Open new block
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
							if part.ThoughtSignature != "" {
								partial.Content[current.index].ThinkingSignature = part.ThoughtSignature
							}
							ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.index, Delta: part.Text, Partial: partial}
						} else {
							partial.Content[current.index].Text += part.Text
							if part.ThoughtSignature != "" {
								partial.Content[current.index].TextSignature = part.ThoughtSignature
							}
							ch <- &goai.TextDeltaEvent{ContentIndex: current.index, Delta: part.Text, Partial: partial}
						}
					}

					// Function call (delivered atomically, not streamed)
					if part.FunctionCall != nil {
						if current != nil {
							closeBlock(current, partial, ch)
							current = nil
						}

						tcID := part.FunctionCall.ID
						if tcID == "" {
							tcID = fmt.Sprintf("%s_%d_%d", part.FunctionCall.Name, time.Now().UnixMilli(), atomic.AddInt64(&toolCallCounter, 1))
						}

						tc := goai.ContentBlock{
							Type:      "toolCall",
							ID:        tcID,
							Name:      part.FunctionCall.Name,
							Arguments: part.FunctionCall.Args,
						}
						if part.ThoughtSignature != "" {
							tc.ThoughtSignature = part.ThoughtSignature
						}

						partial.Content = append(partial.Content, tc)
						idx := len(partial.Content) - 1

						ch <- &goai.ToolCallStartEvent{ContentIndex: idx, Partial: partial}
						argsJSON, _ := json.Marshal(tc.Arguments)
						ch <- &goai.ToolCallDeltaEvent{ContentIndex: idx, Delta: string(argsJSON), Partial: partial}
						ch <- &goai.ToolCallEndEvent{
							ContentIndex: idx,
							ToolCall: goai.ToolCall{
								Type: "toolCall", ID: tcID, Name: tc.Name, Arguments: tc.Arguments,
							},
							Partial: partial,
						}
					}
				}
			}

			// Finish reason
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

		// Usage
		if chunk.UsageMetadata != nil {
			u := chunk.UsageMetadata
			partial.Usage = &goai.Usage{
				Input:       u.PromptTokenCount - u.CachedContentTokenCount,
				Output:      u.CandidatesTokenCount + u.ThoughtsTokenCount,
				CacheRead:   u.CachedContentTokenCount,
				TotalTokens: u.TotalTokenCount,
			}
			partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
		}
	}

	// Close any open block
	if current != nil {
		closeBlock(current, partial, ch)
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}

	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

type currentBlockT struct {
	blockType string
	index     int
}

func closeBlock(current *currentBlockT, partial *goai.Message, ch chan<- goai.Event) {
	switch current.blockType {
	case "text":
		ch <- &goai.TextEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Text, Partial: partial}
	case "thinking":
		ch <- &goai.ThinkingEndEvent{ContentIndex: current.index, Content: partial.Content[current.index].Thinking, Partial: partial}
	}
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

// parseStreamingJSON is a local alias for the shared parser.
var _ = jsonparse.ParsePartialJSON

// --- Google thinking config helpers (upstream parity) ---

var (
	gemini3ProRe   = regexp.MustCompile(`(?i)gemini-3(?:\.\d+)?-pro`)
	gemini3FlashRe = regexp.MustCompile(`(?i)gemini-3(?:\.\d+)?-flash`)
	gemma4Re       = regexp.MustCompile(`(?i)gemma-?4`)
)

func isGemini3ProModel(model *goai.Model) bool {
	return gemini3ProRe.MatchString(model.ID)
}

func isGemini3FlashModel(model *goai.Model) bool {
	id := strings.ToLower(model.ID)
	return gemini3FlashRe.MatchString(id) || id == "gemini-flash-latest" || id == "gemini-flash-lite-latest"
}

func isGemma4Model(model *goai.Model) bool {
	return gemma4Re.MatchString(model.ID)
}

func getDisabledThinkingConfig(model *goai.Model) *geminiThinkingConfig {
	if isGemini3ProModel(model) {
		return &geminiThinkingConfig{ThinkingLevel: "LOW"}
	}
	if isGemini3FlashModel(model) {
		return &geminiThinkingConfig{ThinkingLevel: "MINIMAL"}
	}
	if isGemma4Model(model) {
		return &geminiThinkingConfig{ThinkingLevel: "MINIMAL"}
	}
	// Gemini 2.x: disable via budget=0
	zero := 0
	return &geminiThinkingConfig{ThinkingBudget: &zero}
}

func getGoogleThinkingLevel(effort string, model *goai.Model) string {
	if isGemini3ProModel(model) {
		switch effort {
		case "minimal", "low":
			return "LOW"
		case "medium", "high":
			return "HIGH"
		}
	}
	if isGemma4Model(model) {
		switch effort {
		case "minimal", "low":
			return "MINIMAL"
		case "medium", "high":
			return "HIGH"
		}
	}
	// Gemini 3 Flash
	switch effort {
	case "minimal":
		return "MINIMAL"
	case "low":
		return "LOW"
	case "medium":
		return "MEDIUM"
	case "high":
		return "HIGH"
	default:
		return strings.ToUpper(effort)
	}
}

func getGoogleBudget(model *goai.Model, effort string) int {
	id := strings.ToLower(model.ID)
	if strings.Contains(id, "2.5-pro") {
		switch effort {
		case "minimal":
			return 128
		case "low":
			return 2048
		case "medium":
			return 8192
		case "high":
			return 32768
		}
	}
	if strings.Contains(id, "2.5-flash-lite") {
		switch effort {
		case "minimal":
			return 512
		case "low":
			return 2048
		case "medium":
			return 8192
		case "high":
			return 24576
		}
	}
	if strings.Contains(id, "2.5-flash") {
		switch effort {
		case "minimal":
			return 128
		case "low":
			return 2048
		case "medium":
			return 8192
		case "high":
			return 24576
		}
	}
	return -1
}
