// Package anthropic implements the Anthropic Messages API provider.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/sse"
)

const defaultBaseURL = "https://api.anthropic.com/v1"
const apiVersion = "2023-06-01"
const fineGrainedToolStreamingBeta = "fine-grained-tool-streaming-2025-05-14"
const interleavedThinkingBeta = "interleaved-thinking-2025-05-14"

var claudeCodeToolCanonicalNames = map[string]string{
	"read":            "Read",
	"write":           "Write",
	"edit":            "Edit",
	"bash":            "Bash",
	"grep":            "Grep",
	"glob":            "Glob",
	"askuserquestion": "AskUserQuestion",
	"enterplanmode":   "EnterPlanMode",
	"exitplanmode":    "ExitPlanMode",
	"killshell":       "KillShell",
	"notebookedit":    "NotebookEdit",
	"skill":           "Skill",
	"task":            "Task",
	"taskoutput":      "TaskOutput",
	"todowrite":       "TodoWrite",
	"webfetch":        "WebFetch",
	"websearch":       "WebSearch",
}

type anthropicCompat struct {
	supportsEagerToolInputStreaming bool
	supportsLongCacheRetention      bool
	supportsToolReferences          bool
	supportsStrictTools             bool
	forceAdaptiveThinking           bool
	allowEmptySignature             bool
}

func getAnthropicCompat(model *goai.Model) anthropicCompat {
	c := anthropicCompat{
		supportsEagerToolInputStreaming: true,
		supportsLongCacheRetention:      true,
		supportsToolReferences:          strings.Contains(model.ID, "opus-4-6") || strings.Contains(model.ID, "opus-4-7") || strings.Contains(model.ID, "opus-4-8"),
	}
	if model.AnthropicCompat != nil {
		if model.AnthropicCompat.SupportsEagerToolInputStreaming != nil {
			c.supportsEagerToolInputStreaming = *model.AnthropicCompat.SupportsEagerToolInputStreaming
		}
		if model.AnthropicCompat.SupportsLongCacheRetention != nil {
			c.supportsLongCacheRetention = *model.AnthropicCompat.SupportsLongCacheRetention
		}
		if model.AnthropicCompat.SupportsToolReferences != nil {
			c.supportsToolReferences = *model.AnthropicCompat.SupportsToolReferences
		}
		if model.AnthropicCompat.SupportsStrictTools != nil {
			c.supportsStrictTools = *model.AnthropicCompat.SupportsStrictTools
		}
		if model.AnthropicCompat.ForceAdaptiveThinking != nil {
			c.forceAdaptiveThinking = *model.AnthropicCompat.ForceAdaptiveThinking
		}
		if model.AnthropicCompat.AllowEmptySignature != nil {
			c.allowEmptySignature = *model.AnthropicCompat.AllowEmptySignature
		}
	}
	return c
}

// resolveCacheControl returns the cache_control annotation to use, or nil if caching is disabled.
func resolveCacheControl(model *goai.Model, opts *goai.StreamOptions) *cacheControl {
	retention := ""
	if opts != nil && opts.CacheRetention != "" {
		retention = string(opts.CacheRetention)
	}
	if retention == "" {
		if goai.GetProviderEnvValue("PI_CACHE_RETENTION", goai.ProviderEnvFromOptions(opts)) == "long" {
			retention = "long"
		} else {
			retention = "short"
		}
	}
	if retention == "none" {
		return nil
	}
	compat := getAnthropicCompat(model)
	cc := &cacheControl{Type: "ephemeral"}
	if retention == "long" && compat.supportsLongCacheRetention {
		cc.TTL = "1h"
	}
	return cc
}

func joinBetas(betas []string) string {
	result := ""
	for i, b := range betas {
		if i > 0 {
			result += ","
		}
		result += b
	}
	return result
}

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

func usesClaudeCodeToolNames(model *goai.Model) bool {
	return model != nil && model.Provider == goai.ProviderGitHubCopilot
}

func toClaudeCodeToolName(name string, model *goai.Model) string {
	if usesClaudeCodeToolNames(model) {
		if canonical, ok := claudeCodeToolCanonicalNames[strings.ToLower(name)]; ok {
			return canonical
		}
	}
	return name
}

func fromClaudeCodeToolName(name string, tools []goai.Tool, model *goai.Model) string {
	if usesClaudeCodeToolNames(model) && len(tools) > 0 {
		lower := strings.ToLower(name)
		for _, tool := range tools {
			if strings.ToLower(tool.Name) == lower {
				return tool.Name
			}
		}
	}
	return name
}

func normalizeAnthropicBaseURL(baseURL string) string {
	if baseURL == "" {
		return defaultBaseURL
	}
	normalized := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(normalized, "/v1") {
		return normalized
	}
	return normalized + "/v1"
}

func streamAnthropic(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "anthropic-messages", "provider", model.Provider, "model", model.ID)

		apiKey := goai.ResolveAPIKey(model, opts)
		authToken := ""
		if model.Provider == goai.ProviderAnthropic {
			authToken = goai.GetProviderEnvValue("ANTHROPIC_AUTH_TOKEN", goai.ProviderEnvFromOptions(opts))
		}
		var optHeaders map[string]string
		var suppressHeaders []string
		if opts != nil {
			optHeaders = opts.Headers
			suppressHeaders = opts.SuppressHeaders
		}
		if apiKey == "" && authToken == "" && !goai.HasAnthropicAuthHeader(goai.MergeProviderHeaders(model.Headers, optHeaders, suppressHeaders)) {
			//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("No API key for provider: %s", model.Provider)}
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
			baseURL = goai.ResolveCloudflareBaseURL(model, goai.ProviderEnvFromOptions(opts))
		}
		baseURL = normalizeAnthropicBaseURL(baseURL)

		req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/messages", bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Anthropic-Version", apiVersion)
		req.Header.Set("Accept", "text/event-stream")
		if authToken != "" && !goai.HasAnthropicAuthHeader(goai.MergeProviderHeaders(model.Headers, optHeaders, suppressHeaders)) {
			req.Header.Set("Authorization", "Bearer "+authToken)
		} else if apiKey != "" {
			if model.Provider == goai.ProviderGitHubCopilot {
				req.Header.Set("Authorization", "Bearer "+apiKey)
				for k, v := range goai.CopilotHeaders() {
					req.Header.Set(k, v)
				}
				for k, v := range goai.BuildCopilotDynamicHeaders(convCtx.Messages) {
					req.Header.Set(k, v)
				}
			} else if model.Provider == goai.ProviderCloudflareAIGateway {
				req.Header.Set("cf-aig-authorization", "Bearer "+apiKey)
			} else {
				req.Header.Set("X-Api-Key", apiKey)
			}
		}

		// Beta features
		var betas []string
		compat := getAnthropicCompat(model)
		if !compat.forceAdaptiveThinking {
			betas = append(betas, interleavedThinkingBeta)
		}
		if !compat.supportsEagerToolInputStreaming && len(convCtx.Tools) > 0 {
			betas = append(betas, fineGrainedToolStreamingBeta)
		}
		if len(betas) > 0 {
			req.Header.Set("Anthropic-Beta", joinBetas(betas))
		}

		goai.ApplyDefaultHeaders(req.Header, model.Headers)
		if opts != nil {
			goai.ApplyHeaders(req.Header, opts.Headers)
			goai.SuppressHeaders(req.Header, opts.SuppressHeaders)
		}

		retryCfg := goai.RetryConfigFromOptions(opts)
		client := retryCfg.NewHTTPClient()
		goai.GetLogger().Debug("HTTP request", "url", req.URL.String(), "provider", model.Provider, "model", model.ID, "retries", retryCfg.MaxRetries)
		resp, err := goai.DoProviderRequestWithRetry(ctx, client, req, retryCfg)
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

		processAnthropicStream(resp.Body, model, convCtx.Tools, ch)
	}()

	return ch
}

// --- Request ---

type anthropicRequest struct {
	Model        string             `json:"model"`
	MaxTokens    int                `json:"max_tokens"`
	System       json.RawMessage    `json:"system,omitempty"`
	Messages     []anthropicMessage `json:"messages"`
	Stream       bool               `json:"stream"`
	Tools        []anthropicTool    `json:"tools,omitempty"`
	Temperature  *float64           `json:"temperature,omitempty"`
	Thinking     *anthropicThinking `json:"thinking,omitempty"`
	OutputConfig *anthropicOutput   `json:"output_config,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

type anthropicOutput struct {
	Effort string `json:"effort,omitempty"`
}

func boolPtr(b bool) *bool { return &b }

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      interface{}     `json:"content,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
	Thinking     string          `json:"thinking,omitempty"`
	Signature    string          `json:"signature,omitempty"`

	// Image
	Source *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type anthropicTool struct {
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	InputSchema         json.RawMessage `json:"input_schema"`
	Strict              bool            `json:"strict,omitempty"`
	CacheControl        *cacheControl   `json:"cache_control,omitempty"`
	EagerInputStreaming *bool           `json:"eager_input_streaming,omitempty"`
	DeferLoading        bool            `json:"defer_loading,omitempty"`
}

func buildRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) anthropicRequest {
	maxTokens := goai.ClampStreamMaxTokens(model, convCtx, opts)
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	req := anthropicRequest{
		Model:     model.ID,
		MaxTokens: maxTokens,
		Stream:    true,
	}

	// System prompt with cache control
	if convCtx.SystemPrompt != "" {
		cc := resolveCacheControl(model, opts)
		sysBlock := struct {
			Type         string        `json:"type"`
			Text         string        `json:"text"`
			CacheControl *cacheControl `json:"cache_control,omitempty"`
		}{
			Type:         "text",
			Text:         goai.SanitizeSurrogates(convCtx.SystemPrompt),
			CacheControl: cc,
		}
		req.System, _ = json.Marshal([]interface{}{sysBlock})
	}

	// Temperature: gate on supportsTemperature and not-thinking (upstream parity)
	thinkingEnabled := opts != nil && opts.Reasoning != nil && *opts.Reasoning != ""
	supportsTemp := true
	if model.AnthropicCompat != nil && model.AnthropicCompat.SupportsTemperature != nil {
		supportsTemp = *model.AnthropicCompat.SupportsTemperature
	}
	if opts != nil && opts.Temperature != nil && !thinkingEnabled && supportsTemp {
		req.Temperature = opts.Temperature
	}

	// Configure thinking mode
	compat := getAnthropicCompat(model)
	if model.Reasoning {
		if thinkingEnabled {
			if compat.forceAdaptiveThinking {
				req.Thinking = &anthropicThinking{Type: "adaptive", Display: "summarized"}
				if opts != nil && opts.Reasoning != nil {
					if effort, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning)); ok && effort != "" {
						req.OutputConfig = &anthropicOutput{Effort: effort}
					}
				}
			} else {
				// Budget-based thinking: adjust max_tokens to accommodate thinking budget
				adjustedMax, budget := goai.AdjustMaxTokensForThinking(maxTokens, model.MaxTokens, *opts.Reasoning, opts.ThinkingBudgets)
				req.MaxTokens = adjustedMax
				req.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: budget, Display: "summarized"}
			}
		} else {
			// Explicitly disable thinking if the model supports an off state. Some
			// upstream model metadata marks off as unsupported with a nil mapping; in
			// that case omit the thinking block entirely.
			if off, ok := model.ThinkingLevelMap[goai.ThinkingOff]; !ok || off != nil {
				req.Thinking = &anthropicThinking{Type: "disabled"}
			}
		}
	}

	// Convert messages with cross-provider normalization
	transformed := goai.TransformMessages(convCtx.Messages, model)
	compatForDeferred := getAnthropicCompat(model)
	canonicalizeOAuthTools := opts != nil && strings.HasPrefix(opts.APIKey, "sk-ant-oat-")
	deferredPlan := goai.PlanDeferredTools(convCtx, compatForDeferred.supportsToolReferences, canonicalizeOAuthTools)
	toolCallIDMap := make(map[string]string) // original → normalized
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
				text := goai.SanitizeSurrogates(extractText(m.Content))
				if resolveCacheControl(model, opts) != nil {
					req.Messages = append(req.Messages, anthropicMessage{Role: "user", Content: []anthropicContentBlock{{Type: "text", Text: text}}})
				} else {
					req.Messages = append(req.Messages, anthropicMessage{
						Role:    "user",
						Content: text,
					})
				}
			}
		case goai.RoleAssistant:
			var blocks []anthropicContentBlock
			compat := getAnthropicCompat(model)
			for _, c := range m.Content {
				switch c.Type {
				case "thinking":
					if strings.TrimSpace(c.Thinking) == "" {
						continue
					}
					if c.ThinkingSignature == "" || strings.TrimSpace(c.ThinkingSignature) == "" {
						if compat.allowEmptySignature {
							blocks = append(blocks, anthropicContentBlock{Type: "thinking", Thinking: goai.SanitizeSurrogates(c.Thinking), Signature: ""})
						} else {
							blocks = append(blocks, anthropicContentBlock{Type: "text", Text: goai.SanitizeSurrogates(c.Thinking)})
						}
					} else {
						blocks = append(blocks, anthropicContentBlock{Type: "thinking", Thinking: goai.SanitizeSurrogates(c.Thinking), Signature: c.ThinkingSignature})
					}
				case "text":
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: c.Text})
				case "toolCall":
					inputJSON, _ := json.Marshal(c.Arguments)
					normID := normalizeAnthropicToolCallID(c.ID)
					if normID != c.ID {
						toolCallIDMap[c.ID] = normID
					}
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    normID,
						Name:  toClaudeCodeToolName(c.Name, model),
						Input: inputJSON,
					})
				}
			}
			req.Messages = append(req.Messages, anthropicMessage{Role: "assistant", Content: blocks})
		case goai.RoleToolResult:
			toolUseID := m.ToolCallID
			if mapped, ok := toolCallIDMap[toolUseID]; ok {
				toolUseID = mapped
			}
			refs := goai.DeferredToolsForMarker(deferredPlan, m.AddedToolNames, canonicalizeOAuthTools)
			resultContent := interface{}(extractText(m.Content))
			if len(refs) > 0 {
				var refBlocks []map[string]string
				for _, t := range refs {
					refBlocks = append(refBlocks, map[string]string{"type": "tool_reference", "tool_name": toClaudeCodeToolName(t.Name, model)})
				}
				resultContent = refBlocks
			}
			blocks := []anthropicContentBlock{{Type: "tool_result", ToolUseID: toolUseID, Content: resultContent, IsError: m.IsError}}
			if len(refs) > 0 {
				for _, b := range m.Content {
					switch b.Type {
					case "text":
						if strings.TrimSpace(b.Text) != "" {
							blocks = append(blocks, anthropicContentBlock{Type: "text", Text: b.Text})
						}
					case "image":
						blocks = append(blocks, anthropicContentBlock{Type: "image", Source: &anthropicImageSource{Type: "base64", MediaType: b.MimeType, Data: b.Data}})
					}
				}
			}
			req.Messages = append(req.Messages, anthropicMessage{Role: "user", Content: blocks})
		}
	}

	// Convert tools
	cc := resolveCacheControl(model, opts)
	compatForTools := getAnthropicCompat(model)
	activeTools := append([]goai.Tool{}, deferredPlan.Immediate...)
	activeTools = append(activeTools, deferredPlan.Deferred...)
	if len(activeTools) == 0 {
		activeTools = convCtx.Tools
	}
	deferredNames := map[string]bool{}
	for _, t := range deferredPlan.Deferred {
		deferredNames[t.Name] = true
	}
	for i, t := range activeTools {
		parameters := t.Parameters
		strictTool := false
		if strict, err := goai.ResolveJSONSchemaStrictSampling(t, compatForTools.supportsStrictTools); err == nil && strict != nil && *strict {
			strictTool = true
			if strictParameters, err := goai.JSONSchemaToolParameters(t, true); err == nil {
				parameters = strictParameters
			}
		}
		tool := anthropicTool{
			Name:        toClaudeCodeToolName(t.Name, model),
			Description: t.Description,
			InputSchema: parameters,
			Strict:      strictTool,
		}
		if compatForTools.supportsEagerToolInputStreaming {
			tool.EagerInputStreaming = boolPtr(true)
		}
		if deferredNames[t.Name] {
			tool.DeferLoading = true
		}
		if cc != nil && i == len(activeTools)-1 {
			tool.CacheControl = cc
		}
		req.Tools = append(req.Tools, tool)
	}

	// Add cache_control to the last user message's last block
	if cc != nil && len(req.Messages) > 0 {
		last := &req.Messages[len(req.Messages)-1]
		if last.Role == "user" {
			if blocks, ok := last.Content.([]anthropicContentBlock); ok && len(blocks) > 0 {
				blocks[len(blocks)-1].CacheControl = cc
				last.Content = blocks
			}
		}
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

func processAnthropicStream(body io.Reader, model *goai.Model, tools []goai.Tool, ch chan<- goai.Event) {
	partial := &goai.Message{
		Role:       goai.RoleAssistant,
		Api:        model.Api,
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      &goai.Usage{},
		StopReason: goai.StopReasonPending,
	}

	ch <- &goai.StartEvent{Partial: partial}

	toolJSON := map[int]string{}
	sawMessageStart := false
	sawMessageStop := false
	events := sse.Parse(body)
	for evt := range events {
		if evt.Event == sse.EventError {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", evt.Data)}
			return
		}
		switch evt.Event {
		case "content_block_start":
			var data struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type      string `json:"type"`
					ID        string `json:"id,omitempty"`
					Name      string `json:"name,omitempty"`
					Text      string `json:"text,omitempty"`
					Thinking  string `json:"thinking,omitempty"`
					Signature string `json:"signature,omitempty"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &data); err != nil {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: anthropicSSEParseError(evt, err)}
				return
			}
			switch data.ContentBlock.Type {
			case "text":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text", Text: data.ContentBlock.Text})
				ch <- &goai.TextStartEvent{ContentIndex: data.Index, Partial: partial}
			case "thinking":
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking", Thinking: data.ContentBlock.Thinking, ThinkingSignature: data.ContentBlock.Signature})
				ch <- &goai.ThinkingStartEvent{ContentIndex: data.Index, Partial: partial}
			case "tool_use":
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall",
					ID:   data.ContentBlock.ID,
					Name: fromClaudeCodeToolName(data.ContentBlock.Name, tools, model),
				})
				ch <- &goai.ToolCallStartEvent{ContentIndex: data.Index, Partial: partial}
			}

		case "content_block_delta":
			var data struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text,omitempty"`
					Thinking    string `json:"thinking,omitempty"`
					Signature   string `json:"signature,omitempty"`
					PartialJSON string `json:"partial_json,omitempty"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &data); err != nil {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: anthropicSSEParseError(evt, err)}
				return
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
			case "signature_delta":
				partial.Content[idx].ThinkingSignature += data.Delta.Signature
			case "input_json_delta":
				toolJSON[idx] += data.Delta.PartialJSON
				if args, ok := jsonparse.ParsePartialJSON(toolJSON[idx]); ok && args != nil {
					partial.Content[idx].Arguments = args
				}
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: idx, Delta: data.Delta.PartialJSON, Partial: partial}
			}

		case "content_block_stop":
			var data struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &data); err != nil {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: anthropicSSEParseError(evt, err)}
				return
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
				if partial.Content[idx].Arguments == nil && toolJSON[idx] != "" {
					if args, ok := jsonparse.ParsePartialJSON(toolJSON[idx]); ok && args != nil {
						partial.Content[idx].Arguments = args
					}
				}
				ch <- &goai.ToolCallEndEvent{
					ContentIndex: idx,
					ToolCall: goai.ToolCall{
						Type: "toolCall", ID: c.ID, Name: c.Name, Arguments: partial.Content[idx].Arguments,
					},
					Partial: partial,
				}
			}

		case "message_delta":
			var data struct {
				Delta struct {
					StopReason  string `json:"stop_reason"`
					StopDetails struct {
						Explanation string `json:"explanation"`
					} `json:"stop_details"`
				} `json:"delta"`
				Usage struct {
					OutputTokens  int `json:"output_tokens"`
					OutputDetails struct {
						ThinkingTokens int `json:"thinking_tokens"`
					} `json:"output_tokens_details"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &data); err != nil {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: anthropicSSEParseError(evt, err)}
				return
			}
			partial.Usage.Output = data.Usage.OutputTokens
			partial.Usage.Reasoning = data.Usage.OutputDetails.ThinkingTokens
			partial.Usage.TotalTokens = partial.Usage.Input + partial.Usage.Output + partial.Usage.CacheRead + partial.Usage.CacheWrite
			if data.Delta.StopReason != "" {
				partial.RawStopReason = data.Delta.StopReason
			}

			switch data.Delta.StopReason {
			case "end_turn", "pause_turn", "stop_sequence":
				partial.StopReason = goai.StopReasonStop
			case "max_tokens":
				partial.StopReason = goai.StopReasonLength
			case "tool_use":
				partial.StopReason = goai.StopReasonToolUse
			case "refusal", "sensitive":
				partial.StopReason = goai.StopReasonError
				if data.Delta.StopDetails.Explanation != "" {
					partial.ErrorMessage = data.Delta.StopDetails.Explanation
				} else {
					partial.ErrorMessage = "The model refused to complete the request"
				}
			}

		case "message_start":
			sawMessageStart = true
			var data struct {
				Message struct {
					ID    string `json:"id"`
					Usage struct {
						InputTokens   int `json:"input_tokens"`
						CacheRead     int `json:"cache_read_input_tokens"`
						CacheCreate   int `json:"cache_creation_input_tokens"`
						CacheCreation struct {
							Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
						} `json:"cache_creation"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(evt.Data), &data); err != nil {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: anthropicSSEParseError(evt, err)}
				return
			}
			partial.ResponseID = data.Message.ID
			partial.Usage.Input = data.Message.Usage.InputTokens
			partial.Usage.CacheRead = data.Message.Usage.CacheRead
			partial.Usage.CacheWrite = data.Message.Usage.CacheCreate
			partial.Usage.CacheWrite1h = data.Message.Usage.CacheCreation.Ephemeral1h

		case "message_stop":
			sawMessageStop = true
		}
	}

	if sawMessageStart && !sawMessageStop {
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("anthropic stream ended before message_stop")}
		return
	}

	partial.Timestamp = time.Now().UnixMilli()
	computeCosts(partial.Usage, model)

	if partial.StopReason == goai.StopReasonPending {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = "Anthropic stream ended without a stop reason"
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("anthropic stream ended without a stop reason")}
		return
	}

	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

func anthropicSSEParseError(evt sse.SSEEvent, err error) error {
	raw := strings.Join(evt.Raw, `\n`)
	if raw == "" {
		raw = "data: " + evt.Data
	}
	//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
	return fmt.Errorf("Could not parse Anthropic SSE event %s: %s; data=%s; raw=%s", evt.Event, err.Error(), evt.Data, raw)
}

func computeCosts(usage *goai.Usage, model *goai.Model) {
	usage.Cost = goai.CalculateCost(model, usage)
}

// normalizeAnthropicToolCallID sanitizes tool call IDs for Anthropic's requirements:
// ^[a-zA-Z0-9_-]+$ max 64 characters.
var anthropicToolCallIDRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func normalizeAnthropicToolCallID(id string) string {
	normalized := anthropicToolCallIDRegex.ReplaceAllString(id, "_")
	if len(normalized) > 64 {
		normalized = normalized[:64]
	}
	return normalized
}
