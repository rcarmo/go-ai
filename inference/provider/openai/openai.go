// Package openai implements the OpenAI Chat Completions API provider.
//
// This handles both native OpenAI and any OpenAI-compatible API
// (Ollama, vLLM, LM Studio, Groq, Cerebras, xAI, OpenRouter, etc.).
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/internal/openroutercost"
	"github.com/rcarmo/go-ai/transports/sse"
)

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiOpenAICompletions,
		Stream:       streamOpenAI,
		StreamSimple: streamOpenAISimple,
	})
}

func streamOpenAISimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamOpenAI(ctx, model, convCtx, opts)
}

func streamOpenAI(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)
	send := goai.NewAssistantEventSender(ch)

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
			send(&goai.ErrorEvent{
				Reason: goai.StopReasonError,
				//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
				Err: fmt.Errorf("No API key for provider: %s", model.Provider),
			})
			return
		}

		baseURL := model.BaseURL
		deployment := model.ID
		if model.Provider == goai.ProviderAzureOpenAI {
			var err error
			baseURL, deployment, _, err = goai.ResolveAzureConfig(model, opts)
			if err != nil {
				send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
				return
			}
		}
		// Deployment is a wire override; callbacks/response attribution retain
		// the catalog identity and callers can still replace the payload.
		compat := goai.DetectCompatForModel(model)
		strictSupported := compat.SupportsStrictMode == nil || *compat.SupportsStrictMode
		for _, tool := range convCtx.Tools {
			if _, err := goai.ResolveJSONSchemaStrictSampling(tool, strictSupported); err != nil {
				send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
				return
			}
		}
		if compat.SupportsOpenAIGrammarTools != nil && *compat.SupportsOpenAIGrammarTools {
			for _, tool := range grammarTranscriptTools(convCtx) {
				if tool.ConstrainedSampling != nil && tool.ConstrainedSampling.Type == "grammar" {
					if _, err := openAIGrammarToolDef(tool, nil); err != nil {
						send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
						return
					}
					property, _ := grammarInputProperty(tool)
					for _, message := range convCtx.Messages {
						if message.Role != goai.RoleAssistant {
							continue
						}
						for _, block := range message.Content {
							if block.Type == "toolCall" && block.Name == tool.Name {
								if _, ok := customGrammarInput(block, property); !ok {
									send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("grammar tool call %q requires string argument %q", tool.Name, property)})
									return
								}
							}
						}
					}
				}
			}
		}
		body := buildRequestBodyWithCompat(model, convCtx, opts, compat)
		body.Model = deployment
		payload, err := goai.InvokeOnPayload(opts, body, model)
		if err != nil {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
			return
		}

		bodyJSON, err := json.Marshal(payload)
		if err != nil {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
			return
		}

		if goai.IsCloudflareProvider(model.Provider) {
			baseURL = goai.ResolveCloudflareBaseURL(model, goai.ProviderEnvFromOptions(opts))
		}
		endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyJSON))
		if err != nil {
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: err})
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
		cacheRetentionForHeaders := goai.CacheRetentionShort
		if opts != nil {
			cacheRetentionForHeaders = goai.ResolveCacheRetention(opts.CacheRetention, opts.Env)
		}
		if compat.SendSessionAffinityHeaders != nil && *compat.SendSessionAffinityHeaders && opts != nil && opts.SessionID != "" && cacheRetentionForHeaders != goai.CacheRetentionNone {
			if compat.SessionAffinityFormat == "openrouter" {
				req.Header.Set("x-session-id", opts.SessionID)
			} else {
				if compat.SessionAffinityFormat == "openai" {
					req.Header.Set("session_id", opts.SessionID)
				}
				req.Header.Set("x-client-request-id", opts.SessionID)
				req.Header.Set("x-session-affinity", opts.SessionID)
			}
		}

		// Dynamic Copilot headers
		if model.Provider == goai.ProviderGitHubCopilot {
			for k, v := range goai.BuildCopilotDynamicHeaders(convCtx.Messages) {
				req.Header.Set(k, v)
			}
		}

		// Apply custom headers
		if opts != nil {
			goai.ApplyHeaders(req.Header, goai.WithOpenCodeSessionHeader(model.Provider, opts.SessionID, opts.Headers))
		}
		goai.ApplyDefaultHeaders(req.Header, goai.WithOpenCodeSessionHeader(model.Provider, goai.SessionIDFromOptions(opts), model.Headers, model.BaseURL))
		goai.ApplyDefaultHeaders(req.Header, goai.RequestIdentityHeaders(model, opts))
		if opts != nil {
			goai.SuppressHeaders(req.Header, opts.SuppressHeaders)
		}

		retryCfg := goai.RetryConfigFromOptions(opts)
		client := retryCfg.NewHTTPClient()
		if opts != nil && opts.HTTPTransport != nil {
			client.Transport = opts.HTTPTransport
		}
		goai.GetLogger().Debug("HTTP request", "url", req.URL.String(), "provider", model.Provider, "model", model.ID, "retries", retryCfg.MaxRetries)
		resp, err := goai.DoObservedProviderStreamRequest(ctx, client, req, retryCfg, opts, func(response *http.Response) {
			goai.InvokeOnResponse(opts, response, model)
		})
		if err != nil {
			if ctx.Err() != nil {
				goai.GetLogger().Debug("request aborted", "provider", model.Provider, "model", model.ID)
				send(&goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()})
			} else {
				goai.GetLogger().Warn("network error", "provider", model.Provider, "model", model.ID, "error", err)
				send(&goai.ErrorEvent{Reason: goai.StopReasonError, Err: openAIConnectionError(err)})
			}
			return
		}
		defer resp.Body.Close()

		goai.InvokeOnResponse(opts, resp, model)

		if resp.StatusCode != 200 {
			goai.GetLogger().Warn("HTTP error response", "status", resp.StatusCode, "provider", model.Provider, "model", model.ID)
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			send(&goai.ErrorEvent{
				Reason: goai.StopReasonError,
				Err:    fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes)),
			})
			return
		}

		processSSEStreamWithOptions(resp.Body, model, opts, send, grammarTranscriptTools(convCtx))
	}()

	return ch
}

// --- Request building ---

type chatRequest struct {
	Model                    string                 `json:"model"`
	Messages                 []chatMessage          `json:"messages"`
	Stream                   bool                   `json:"stream"`
	StreamOptions            *streamOpts            `json:"stream_options,omitempty"`
	PromptCacheKey           string                 `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention     string                 `json:"prompt_cache_retention,omitempty"`
	Temperature              *float64               `json:"temperature,omitempty"`
	MaxTokens                *int                   `json:"max_tokens,omitempty"`
	MaxCompletionToks        *int                   `json:"max_completion_tokens,omitempty"`
	Tools                    []toolDef              `json:"-"`
	EmitEmptyTools           bool                   `json:"-"`
	ReasoningEffort          string                 `json:"reasoning_effort,omitempty"`
	ToolChoice               string                 `json:"tool_choice,omitempty"`
	Reasoning                map[string]interface{} `json:"reasoning,omitempty"`
	Thinking                 map[string]interface{} `json:"thinking,omitempty"`
	EnableThinking           *bool                  `json:"enable_thinking,omitempty"`
	ChatTemplateKwargs       map[string]interface{} `json:"chat_template_kwargs,omitempty"`
	ChatTemplateArgs         map[string]interface{} `json:"chat_template_args,omitempty"`
	ThinkingTokenBudget      *int                   `json:"thinking_token_budget,omitempty"`
	ThinkingTokenBudgetField string                 `json:"-"`
	ToolStream               *bool                  `json:"tool_stream,omitempty"`
	Store                    *bool                  `json:"store,omitempty"`
	Priority                 *int                   `json:"priority,omitempty"`
	SamplingParams           map[string]interface{} `json:"-"`
}

func (r chatRequest) MarshalJSON() ([]byte, error) {
	type alias chatRequest
	m := map[string]interface{}{}
	b, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if len(r.Tools) > 0 {
		m["tools"] = r.Tools
	} else if r.EmitEmptyTools {
		m["tools"] = []toolDef{}
	}
	if r.ThinkingTokenBudgetField != "" && r.ThinkingTokenBudget != nil && r.ThinkingTokenBudgetField != "thinking_token_budget" {
		delete(m, "thinking_token_budget")
		m[r.ThinkingTokenBudgetField] = *r.ThinkingTokenBudget
	}
	for k, v := range r.SamplingParams {
		m[k] = v
	}
	return json.Marshal(m)
}

type streamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role             string            `json:"role"`
	Content          interface{}       `json:"content"` // string or []contentPart
	ReasoningDetails []reasoningDetail `json:"reasoning_details,omitempty"`
	ReasoningContent *string           `json:"reasoning_content,omitempty"`
	Reasoning        *string           `json:"reasoning,omitempty"`
	ReasoningText    *string           `json:"reasoning_text,omitempty"`
	ReasoningOpaque  string            `json:"reasoning_opaque,omitempty"`
	ToolCalls        []toolCallPart    `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	Name             string            `json:"name,omitempty"`
	Tools            []toolDef         `json:"tools,omitempty"`
	OmitContent      bool              `json:"-"`
}

func (m chatMessage) MarshalJSON() ([]byte, error) {
	type alias chatMessage
	raw := alias(m)
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if !m.OmitContent {
		return b, nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, err
	}
	delete(obj, "content")
	return json.Marshal(obj)
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
	Type         string                 `json:"type"`
	Function     *toolFunction          `json:"function,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Format       map[string]interface{} `json:"format,omitempty"`
	CacheControl *cacheControl          `json:"cache_control,omitempty"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

func openAIGrammarToolDef(t goai.Tool, cacheMarker *cacheControl) (toolDef, error) {
	if _, err := grammarInputProperty(t); err != nil {
		return toolDef{}, err
	}
	definition := t.ConstrainedSampling.Variants["openai_lark"]
	syntax := "lark"
	if strings.TrimSpace(definition) == "" {
		definition = t.ConstrainedSampling.Variants["openai_regex"]
		syntax = "regex"
	}
	if strings.TrimSpace(definition) == "" {
		return toolDef{}, fmt.Errorf("tool %q cannot use grammar constrained sampling", t.Name)
	}
	return toolDef{Type: "custom", Name: t.Name, Format: map[string]interface{}{"type": "grammar", "syntax": syntax, "definition": definition}, CacheControl: cacheMarker}, nil
}

func convertToolDefs(tools []goai.Tool, compat goai.OpenAICompletionsCompat, cacheMarker *cacheControl) []toolDef {
	if len(tools) == 0 {
		return nil
	}
	strictMode := compat.SupportsStrictMode == nil || *compat.SupportsStrictMode
	defs := make([]toolDef, 0, len(tools))
	for _, t := range tools {
		if t.ConstrainedSampling != nil && t.ConstrainedSampling.Type == "grammar" && compat.SupportsOpenAIGrammarTools != nil && *compat.SupportsOpenAIGrammarTools {
			if def, err := openAIGrammarToolDef(t, cacheMarker); err == nil {
				defs = append(defs, def)
				continue
			}
		}
		parameters := t.Parameters
		strictForTool := strictMode
		if t.ConstrainedSampling != nil && t.ConstrainedSampling.Type == "json_schema" {
			strictForTool = false
		}
		if strict, err := goai.ResolveJSONSchemaStrictSampling(t, strictMode); err == nil && strict != nil {
			strictForTool = *strict
			if *strict {
				if strictParameters, err := goai.JSONSchemaToolParameters(t, true); err == nil {
					parameters = strictParameters
				}
			}
		}
		defs = append(defs, toolDef{
			Type:         "function",
			CacheControl: cacheMarker,
			Function: &toolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  parameters,
				Strict:      strictForTool,
			},
		})
	}
	return defs
}

type toolCallPart struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Function toolCallFunction  `json:"function,omitzero"`
	Custom   map[string]string `json:"custom,omitempty"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func buildRequestBody(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) chatRequest {
	return buildRequestBodyWithCompat(model, convCtx, opts, goai.DetectCompatForModel(model))
}

func buildRequestBodyWithCompat(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions, compat goai.OpenAICompletionsCompat) chatRequest {
	convCtx = goai.ResolveContext(convCtx, compat.SupportsMidConvoSystemMessages != nil && *compat.SupportsMidConvoSystemMessages)

	req := chatRequest{
		Model:          model.ID,
		Stream:         true,
		SamplingParams: mergeSamplingParams(model, opts),
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
	longRetention := cacheRetention == goai.CacheRetentionLong && (compat.SupportsLongCacheRetention == nil || *compat.SupportsLongCacheRetention)
	mode := compat.PromptCacheKeyMode
	supportsPromptCacheKey := mode == "enabled" || ((mode == "" || mode == "auto") && (strings.Contains(model.BaseURL, "api.openai.com") || longRetention))
	if opts != nil && opts.SessionID != "" && cacheRetention != goai.CacheRetentionNone && supportsPromptCacheKey {
		req.PromptCacheKey = goai.ClampOpenAIPromptCacheKey(opts.SessionID)
	}
	if longRetention {
		req.PromptCacheRetention = "24h"
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		clampedMaxTokens := goai.ClampStreamMaxTokensPtr(model, convCtx, opts)
		// Max tokens field depends on provider.
		if compat.MaxTokensField == "max_completion_tokens" {
			req.MaxCompletionToks = clampedMaxTokens
		} else {
			req.MaxTokens = clampedMaxTokens
		}
	}

	// Store field
	if compat.SupportsStore != nil && *compat.SupportsStore {
		t := true
		req.Store = &t
	}
	if opts != nil && opts.ToolChoice != "" {
		req.ToolChoice = string(opts.ToolChoice)
	}
	if compat.VLLMPriority != nil {
		req.Priority = compat.VLLMPriority
	}

	// Reasoning effort / thinking control. Native empty/off pointers represent
	// no requested effort, just like an absent source reasoning option.
	reasoningRequested := opts != nil && opts.Reasoning != nil && *opts.Reasoning != "" && *opts.Reasoning != goai.ThinkingLevel(goai.ThinkingOff)
	effort := ""
	if reasoningRequested {
		if mapped, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning)); ok {
			effort = mapped
		}
	}
	if model.Reasoning {
		switch compat.ThinkingFormat {
		case "zai":
			if reasoningRequested && effort != "" {
				req.Thinking = map[string]interface{}{"type": "enabled", "clear_thinking": false}
				if compat.SupportsReasoningEffort != nil && *compat.SupportsReasoningEffort {
					req.ReasoningEffort = effort
				}
			} else {
				req.Thinking = map[string]interface{}{"type": "disabled"}
			}
		case "qwen":
			enabled := reasoningRequested && effort != ""
			req.EnableThinking = &enabled
			if enabled && (compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort) {
				req.ReasoningEffort = effort
			}
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
			if kwargs := buildChatTemplateValues(model, opts, effort, compat.ChatTemplateKwargs); len(kwargs) > 0 {
				req.ChatTemplateKwargs = kwargs
			}
		case "baseten":
			if args := buildChatTemplateValues(model, opts, effort, compat.ChatTemplateArgs); len(args) > 0 {
				req.ChatTemplateArgs = args
			}
			if compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort {
				if effort != "" {
					req.ReasoningEffort = effort
				} else if off, ok := model.ThinkingLevelMap[goai.ThinkingOff]; ok && off != nil {
					req.ReasoningEffort = *off
				}
			}
		case "deepseek":
			if reasoningRequested {
				req.Thinking = map[string]interface{}{"type": "enabled"}
				if effort != "" && (compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort) {
					req.ReasoningEffort = effort
				}
			} else if off, declared := model.ThinkingLevelMap[goai.ThinkingOff]; !declared || off != nil {
				// Explicit null means this model cannot turn thinking off.
				req.Thinking = map[string]interface{}{"type": "disabled"}
			}
		case "openrouter":
			if reasoningRequested && effort != "" {
				req.Reasoning = map[string]interface{}{"effort": effort}
			} else if off, present := model.ThinkingLevelMap[goai.ThinkingOff]; !present || off != nil {
				// Missing metadata permits explicit disabling; an explicit null
				// marks mandatory reasoning and must leave the control absent.
				value := "none"
				if off != nil {
					value = *off
				}
				req.Reasoning = map[string]interface{}{"effort": value}
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
	req.Messages = convertMessages(model, convCtx, &compat, grammarTranscriptTools(convCtx))
	if useAnthropicCacheControl {
		applyAnthropicCacheControl(req.Messages, cacheMarker)
	}

	// Convert tools. Match upstream OpenAI-compatible behavior: omit tools for
	// empty/no-tool contexts, but preserve an explicit empty tools array when the
	// conversation contains prior tool-call/tool-result history for proxy replay.
	activeTools := convCtx.Tools
	if compat.DeferredToolsMode == "kimi" {
		deferred := getDeferredToolNames(convCtx.Messages)
		if len(deferred) > 0 {
			activeTools = activeTools[:0]
			for _, tool := range convCtx.Tools {
				if !deferred[tool.Name] {
					activeTools = append(activeTools, tool)
				}
			}
		}
	}
	thinkingBudgetField := compat.ThinkingTokenBudgetField
	if thinkingBudgetField == "" && compat.SupportsThinkingTokenBudget != nil && *compat.SupportsThinkingTokenBudget {
		thinkingBudgetField = "thinking_token_budget"
	}
	if thinkingBudgetField != "" && reasoningRequested && model.Reasoning && opts != nil && opts.Reasoning != nil {
		ceiling := model.MaxTokens
		if opts.MaxTokens != nil {
			ceiling = goai.ClampStreamMaxTokens(model, convCtx, opts)
		}
		_, budget := goai.AdjustThinkingTokenBudget(ceiling, *opts.Reasoning, opts.ThinkingBudgets)
		if budget > 0 {
			req.ThinkingTokenBudget = &budget
			req.ThinkingTokenBudgetField = thinkingBudgetField
		}
	}

	if len(activeTools) > 0 {
		if compat.ZaiToolStream != nil && *compat.ZaiToolStream {
			t := true
			req.ToolStream = &t
		}
		req.Tools = append(req.Tools, convertToolDefs(activeTools, compat, cacheMarker)...)
	} else if compat.RequiresToolsWithToolHistory != nil && *compat.RequiresToolsWithToolHistory && hasToolHistory(convCtx) {
		req.EmitEmptyTools = true
	}

	return req
}

func getDeferredToolNames(messages []goai.Message) map[string]bool {
	names := map[string]bool{}
	for _, msg := range messages {
		if msg.Role != goai.RoleToolResult {
			continue
		}
		for _, name := range msg.AddedToolNames {
			if name != "" {
				names[name] = true
			}
		}
	}
	return names
}

func toolsByName(tools []goai.Tool, names map[string]bool) []goai.Tool {
	if len(names) == 0 || len(tools) == 0 {
		return nil
	}
	out := make([]goai.Tool, 0, len(names))
	for _, tool := range tools {
		if names[tool.Name] {
			out = append(out, tool)
		}
	}
	return out
}

func hasToolHistory(convCtx *goai.Context) bool {
	if convCtx == nil {
		return false
	}
	for _, msg := range convCtx.Messages {
		if msg.Role == goai.RoleToolResult || msg.ToolCallID != "" {
			return true
		}
		for _, c := range msg.Content {
			if c.Type == "toolCall" || c.ID != "" {
				return true
			}
		}
	}
	return false
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

func mergeSamplingParams(model *goai.Model, opts *goai.StreamOptions) map[string]interface{} {
	level := goai.ThinkingOff
	var overrides map[string]any
	if opts != nil {
		if opts.Reasoning != nil {
			level = goai.ModelThinkingLevel(*opts.Reasoning)
		}
		overrides = opts.SamplingParams
	}
	return goai.ResolveSamplingParams(model, level, overrides)
}

func buildChatTemplateValues(model *goai.Model, opts *goai.StreamOptions, effort string, values map[string]goai.ChatTemplateKwargValue) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(values))
	for key, value := range values {
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

func resolveChatTemplateKwargValue(model *goai.Model, opts *goai.StreamOptions, effort string, value goai.ChatTemplateKwargValue) (interface{}, bool) {
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
	case "thinking.budget":
		if opts == nil || opts.Reasoning == nil || *opts.Reasoning == "" || *opts.Reasoning == goai.ThinkingLevel(goai.ThinkingOff) || !model.Reasoning || effort == "" {
			// An unavailable source budget is undefined and omits the key; JSON
			// null is not equivalent, even when omitWhenOff was not requested.
			return nil, false
		}
		ceiling := model.MaxTokens
		if opts.MaxTokens != nil {
			ceiling = goai.ClampStreamMaxTokens(model, &goai.Context{}, opts)
		}
		_, budget := goai.AdjustThinkingTokenBudget(ceiling, *opts.Reasoning, opts.ThinkingBudgets)
		if budget <= 0 {
			return nil, false
		}
		return budget, true
	default:
		return value.Value, true
	}
}

func convertMessages(model *goai.Model, convCtx *goai.Context, compat *goai.OpenAICompletionsCompat, toolSets ...[]goai.Tool) []chatMessage {
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
		case goai.RoleSystem:
			role := "system"
			if model.Reasoning && compat.SupportsDeveloperRole != nil && *compat.SupportsDeveloperRole {
				role = "developer"
			}
			if text := goai.RenderSystemMessageText(m); strings.TrimSpace(text) != "" {
				msgs = append(msgs, chatMessage{Role: role, Content: goai.SanitizeSurrogates(text)})
			}
			if len(m.ToolsAdded) > 0 && compat.SupportsMidConvoToolAdditions != nil && *compat.SupportsMidConvoToolAdditions {
				msgs = append(msgs, chatMessage{Role: "system", Tools: convertToolDefs(m.ToolsAdded, *compat, nil), OmitContent: true})
			}

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
						if b.Text == "" {
							continue
						}
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
			var opaque opaqueReasoningSignature
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
					if decoded, ok := decodeOpaqueReasoningSignature(c.ThinkingSignature); ok {
						opaque = decoded
					} else if details := validReplayReasoningDetails(c.ThinkingSignature); len(details) > 0 {
						msg.ReasoningDetails = append(msg.ReasoningDetails, details...)
					}
				case "toolCall":
					argsJSON, _ := json.Marshal(c.Arguments)
					toolCallID := normalizeOpenAIToolCallID(c.ID)
					if details := validReplayReasoningDetails(c.ThoughtSignature); len(details) > 0 {
						msg.ReasoningDetails = append(msg.ReasoningDetails, details...)
					}
					property, selected := grammarInputPropertyFor(c.Name, toolSets)
					if selected && compat.SupportsOpenAIGrammarTools != nil && *compat.SupportsOpenAIGrammarTools {
						input, ok := customGrammarInput(c, property)
						if ok {
							msg.ToolCalls = append(msg.ToolCalls, toolCallPart{ID: toolCallID, Type: "custom", Custom: map[string]string{"name": c.Name, "input": goai.SanitizeSurrogates(input)}})
							continue
						}
					}
					msg.ToolCalls = append(msg.ToolCalls, toolCallPart{
						ID:   toolCallID,
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
				// Convert thinking to distinct text parts so replay preserves upstream
				// OpenAI-compatible content shape instead of collapsing blocks.
				parts := make([]map[string]any, 0, len(thinkingParts)+len(textParts))
				for _, text := range thinkingParts {
					parts = append(parts, map[string]any{"type": "text", "text": goai.SanitizeSurrogates(text)})
				}
				for _, text := range textParts {
					parts = append(parts, map[string]any{"type": "text", "text": goai.SanitizeSurrogates(text)})
				}
				msg.Content = parts
			} else if len(textParts) > 0 {
				msg.Content = goai.SanitizeSurrogates(joinStrings(textParts))
			}

			if len(msg.ReasoningDetails) == 0 {
				msg.ReasoningOpaque = opaque.Opaque
				if len(thinkingParts) > 0 && (compat.RequiresThinkingAsText == nil || !*compat.RequiresThinkingAsText) {
					field := opaque.Field
					if opaque.Opaque == "" {
						for _, block := range m.Content {
							if block.Type == "thinking" && block.Thinking != "" {
								field = block.ThinkingSignature
								break
							}
						}
					}
					value := strings.Join(thinkingParts, "\n")
					switch completionsReasoningField(model.Provider, field) {
					case "reasoning_content":
						msg.ReasoningContent = &value
					case "reasoning":
						msg.Reasoning = &value
					case "reasoning_text":
						msg.ReasoningText = &value
					}
				}
			}
			if msg.ReasoningContent == nil && model.Reasoning && compat.RequiresReasoningContentOnAssistantMessages != nil && *compat.RequiresReasoningContentOnAssistantMessages {
				value := ""
				msg.ReasoningContent = &value
			}

			// Skip empty assistant messages with no tool calls
			if msg.Content == nil && len(msg.ToolCalls) == 0 {
				continue
			}
			if msg.Content == nil && ((compat.RequiresAssistantAfterToolResult != nil && *compat.RequiresAssistantAfterToolResult) ||
				(compat.RequiresNonNullAssistantContent != nil && *compat.RequiresNonNullAssistantContent)) {
				msg.Content = ""
			}
			msgs = append(msgs, msg)

		case goai.RoleToolResult:
			deferredToolNames := map[string]bool{}
			text := goai.ContentText(m.Content)
			if strings.TrimSpace(text) == "" {
				text = "(no tool output)"
			}
			toolMsg := chatMessage{
				Role:       "tool",
				Content:    goai.SanitizeSurrogates(text),
				ToolCallID: normalizeOpenAIToolCallID(m.ToolCallID),
			}
			// Some providers require the name field
			if compat.RequiresToolResultName != nil && *compat.RequiresToolResultName && m.ToolName != "" {
				toolMsg.Name = m.ToolName
			}
			msgs = append(msgs, toolMsg)
			if compat.DeferredToolsMode == "kimi" {
				for _, name := range m.AddedToolNames {
					if name != "" {
						deferredToolNames[name] = true
					}
				}
			}

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
			if len(deferredToolNames) > 0 {
				if deferredTools := toolsByName(convCtx.Tools, deferredToolNames); len(deferredTools) > 0 {
					msgs = append(msgs, chatMessage{Role: "system", Tools: convertToolDefs(deferredTools, *compat, nil), OmitContent: true})
				}
			}
		}

		lastRole = m.Role
	}

	return msgs
}
func normalizeOpenAIToolCallID(id string) string {
	if idx := strings.Index(id, "|"); idx >= 0 {
		return id[:idx]
	}
	return id
}

func extractTextContent(blocks []goai.ContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

// OpenCode Go may stream the reasoning alias, but it expects reasoning_content
// when assistant history is replayed.
func completionsReasoningField(provider goai.Provider, field string) string {
	if provider == goai.ProviderOpenCodeGo && field == "reasoning" {
		return "reasoning_content"
	}
	return field
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
	Index              int             `json:"index"`
	Delta              sseDelta        `json:"delta"`
	FinishReason       *string         `json:"finish_reason"`
	NativeFinishReason json.RawMessage `json:"native_finish_reason"`
	Usage              *sseUsage       `json:"usage,omitempty"`
}

type sseDelta struct {
	Role             string            `json:"role,omitempty"`
	Content          *string           `json:"content,omitempty"`
	ToolCalls        []sseToolCall     `json:"tool_calls,omitempty"`
	Reasoning        *string           `json:"reasoning,omitempty"`
	ReasoningContent *string           `json:"reasoning_content,omitempty"`
	ReasoningText    *string           `json:"reasoning_text,omitempty"`
	ReasoningDetails []reasoningDetail `json:"reasoning_details,omitempty"`
	ReasoningOpaque  json.RawMessage   `json:"reasoning_opaque,omitempty"`
}

// Copilot stores opaque replay data on a thinking block. The optional field
// identifies the wire field for its text; a value can also arrive without text.
type opaqueReasoningSignature struct {
	Field  string `json:"field,omitempty"`
	Opaque string `json:"reasoning_opaque"`
}

func decodeOpaqueReasoningSignature(signature string) (opaqueReasoningSignature, bool) {
	if signature == "" || signature[0] != '{' {
		return opaqueReasoningSignature{}, false
	}
	var stored map[string]json.RawMessage
	var decoded opaqueReasoningSignature
	if json.Unmarshal([]byte(signature), &stored) != nil || json.Unmarshal(stored["reasoning_opaque"], &decoded.Opaque) != nil || decoded.Opaque == "" {
		return opaqueReasoningSignature{}, false
	}
	field, present := stored["field"]
	if !present {
		return decoded, true
	}
	if json.Unmarshal(field, &decoded.Field) == nil {
		switch decoded.Field {
		case "reasoning_content", "reasoning", "reasoning_text":
			return decoded, true
		}
	}
	return opaqueReasoningSignature{}, false
}

type reasoningDetail struct {
	Type      string  `json:"type"`
	ID        *string `json:"id,omitempty"`
	Format    string  `json:"format,omitempty"`
	Index     *int    `json:"index,omitempty"`
	Data      string  `json:"data,omitempty"`
	Text      string  `json:"text,omitempty"`
	Signature *string `json:"signature,omitempty"`
	Summary   string  `json:"summary,omitempty"`
}

func isEncryptedReasoningDetail(detail reasoningDetail) bool {
	return detail.Type == "reasoning.encrypted" && detail.ID != nil && *detail.ID != "" && detail.Data != ""
}

func appendReasoningDetail(details []reasoningDetail, detail reasoningDetail) []reasoningDetail {
	if len(details) > 0 {
		last := &details[len(details)-1]
		switch {
		case detail.Type == "reasoning.text" && last.Type == "reasoning.text":
			last.Text += detail.Text
			if last.Signature == nil {
				last.Signature = detail.Signature
			}
			fillReasoningDetailCommon(last, detail)
			return details
		case detail.Type == "reasoning.summary" && last.Type == "reasoning.summary":
			last.Summary += detail.Summary
			fillReasoningDetailCommon(last, detail)
			return details
		}
	}
	return append(details, detail)
}

func fillReasoningDetailCommon(target *reasoningDetail, source reasoningDetail) {
	if target.ID == nil {
		target.ID = source.ID
	}
	if target.Format == "" {
		target.Format = source.Format
	}
	if target.Index == nil {
		target.Index = source.Index
	}
}

func validReplayReasoningDetails(signature string) []reasoningDetail {
	if signature == "" {
		return nil
	}
	var details []reasoningDetail
	if err := json.Unmarshal([]byte(signature), &details); err == nil && len(details) > 0 {
		for _, detail := range details {
			if !isValidReasoningDetail(detail) {
				return nil
			}
		}
		return details
	}
	var detail reasoningDetail
	if err := json.Unmarshal([]byte(signature), &detail); err == nil && isEncryptedReasoningDetail(detail) {
		return []reasoningDetail{detail}
	}
	return nil
}

func isValidReasoningDetail(detail reasoningDetail) bool {
	switch detail.Type {
	case "reasoning.encrypted":
		return isEncryptedReasoningDetail(detail)
	case "reasoning.text":
		return detail.Text != ""
	case "reasoning.summary":
		return detail.Summary != ""
	default:
		return false
	}
}

type sseToolCall struct {
	Index    *int            `json:"index,omitempty"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function sseToolFunction `json:"function"`
	Custom   sseToolCustom   `json:"custom"`
}

type sseToolFunction struct {
	Name      string                 `json:"name,omitempty"`
	Arguments jsonparse.ArgumentText `json:"arguments,omitempty"`
}

type sseToolCustom struct {
	Name  string                 `json:"name,omitempty"`
	Input jsonparse.ArgumentText `json:"input,omitempty"`
}

type sseUsage struct {
	openroutercost.Accounting
	PromptTokens            int  `json:"prompt_tokens"`
	CompletionTokens        int  `json:"completion_tokens"`
	ReasoningTokens         int  `json:"reasoning_tokens"`
	TotalTokens             int  `json:"total_tokens"`
	PromptCacheHitTokens    *int `json:"prompt_cache_hit_tokens"`
	CachedTokens            *int `json:"cached_tokens"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
	PromptTokensDetails *struct {
		CachedTokens     *int `json:"cached_tokens"`
		CacheWriteTokens int  `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
}

func processSSEStream(body io.Reader, model *goai.Model, send func(goai.Event)) {
	processSSEStreamWithOptions(body, model, nil, send)
}

func processSSEStreamWithOptions(body io.Reader, model *goai.Model, opts *goai.StreamOptions, send func(goai.Event), toolSets ...[]goai.Tool) {
	partial := &goai.Message{
		Role:       goai.RoleAssistant,
		Api:        model.Api,
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      &goai.Usage{},
		StopReason: goai.StopReasonPending,
	}

	send(&goai.StartEvent{Partial: partial})

	// Track active tool calls for argument accumulation
	type activeToolCall struct {
		indexed        bool
		id             string
		name           string
		argumentStream jsonparse.ToolCallArgumentsStream
		customStream   jsonparse.GrammarToolInputStream
		custom         bool
		contentIdx     int
	}
	var activeTools []*activeToolCall
	toolsByIndex := make(map[int]*activeToolCall)
	toolsByID := make(map[string]*activeToolCall)
	var streamedReasoningDetails []reasoningDetail
	var streamedReasoning opaqueReasoningSignature
	var thinkingBlockIdx = -1
	var textBlockIdx = -1
	var finishReason *string
	var nativeReason, nativeAtFinish string
	isOpenRouter := model.Provider == goai.ProviderOpenRouter || strings.Contains(model.BaseURL, "openrouter.ai")
	ensureThinkingBlock := func() int {
		if thinkingBlockIdx < 0 {
			partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
			thinkingBlockIdx = len(partial.Content) - 1
			send(goai.SnapshotEvent(&goai.ThinkingStartEvent{ContentIndex: thinkingBlockIdx, Partial: partial}))
		}
		return thinkingBlockIdx
	}
	applyReasoningReplayData := func() {
		if thinkingBlockIdx < 0 {
			return
		}
		var replay any
		if len(streamedReasoningDetails) > 0 {
			replay = streamedReasoningDetails
		} else if streamedReasoning.Opaque != "" {
			replay = streamedReasoning
		} else {
			return
		}
		if signature, err := json.Marshal(replay); err == nil {
			partial.Content[thinkingBlockIdx].ThinkingSignature = string(signature)
		}
	}
	failStream := func(err error) {
		applyReasoningReplayData()
		send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
	}

	events := sse.Parse(body)
	for evt := range events {
		if evt.Event == sse.EventError {
			failStream(fmt.Errorf("SSE stream error: %s", evt.Data))
			return
		}
		if evt.Data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
			continue
		}
		if opts != nil && opts.OnProviderStreamEvent != nil {
			if err := opts.OnProviderStreamEvent(chunk, model); err != nil {
				failStream(err)
				return
			}
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

		if isOpenRouter && len(choice.NativeFinishReason) != 0 {
			var reported string
			if json.Unmarshal(choice.NativeFinishReason, &reported) == nil && reported != "" {
				nativeReason = reported
			}
		}
		if choice.FinishReason != nil {
			finishReason = choice.FinishReason
			nativeAtFinish = nativeReason
			partial.RawStopReason = *choice.FinishReason
		}

		// Thinking/reasoning content — check fields in priority order
		// (reasoning_content for llama.cpp, reasoning for other endpoints, reasoning_text)
		var reasoningDelta, reasoningField string
		if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
			reasoningDelta, reasoningField = *delta.ReasoningContent, "reasoning_content"
		} else if delta.Reasoning != nil && *delta.Reasoning != "" {
			reasoningDelta, reasoningField = *delta.Reasoning, "reasoning"
		} else if delta.ReasoningText != nil && *delta.ReasoningText != "" {
			reasoningDelta, reasoningField = *delta.ReasoningText, "reasoning_text"
		}
		if reasoningDelta != "" {
			streamedReasoning.Field = completionsReasoningField(model.Provider, reasoningField)
			idx := ensureThinkingBlock()
			partial.Content[idx].Thinking += reasoningDelta
			if partial.Content[idx].ThinkingSignature == "" {
				partial.Content[idx].ThinkingSignature = streamedReasoning.Field
			}
			send(goai.SnapshotEvent(&goai.ThinkingDeltaEvent{
				ContentIndex: idx,
				Delta:        reasoningDelta,
				Partial:      partial,
			}))
		}

		// Read metadata before visible text so an opaque-only block has the
		// same position as a reasoning block with deltas.
		if len(delta.ReasoningOpaque) > 0 {
			var opaque string
			if json.Unmarshal(delta.ReasoningOpaque, &opaque) == nil && opaque != "" {
				ensureThinkingBlock()
				streamedReasoning.Opaque = opaque
			}
		}
		// reasoning_details are replay metadata. Keep them on a single thinking
		// block signature, merging adjacent text/summary deltas in stream order.
		for _, detail := range delta.ReasoningDetails {
			if !isValidReasoningDetail(detail) {
				continue
			}
			streamedReasoningDetails = appendReasoningDetail(streamedReasoningDetails, detail)
			ensureThinkingBlock()
		}
		// Text content
		if delta.Content != nil && *delta.Content != "" {
			if textBlockIdx < 0 {
				partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
				textBlockIdx = len(partial.Content) - 1
				send(goai.SnapshotEvent(&goai.TextStartEvent{
					ContentIndex: textBlockIdx,
					Partial:      partial,
				}))
			}
			idx := textBlockIdx
			partial.Content[idx].Text += *delta.Content
			send(goai.SnapshotEvent(&goai.TextDeltaEvent{
				ContentIndex: idx,
				Delta:        *delta.Content,
				Partial:      partial,
			}))
		}

		// Tool calls
		for _, tc := range delta.ToolCalls {
			name := tc.Function.Name
			isCustom := tc.Type == "custom" || tc.Custom.Name != "" || tc.Custom.Input != ""
			if name == "" {
				name = tc.Custom.Name
			}
			// Prefer the provider index when present, with ID fallback for
			// providers that leave it out. A missing index is not index zero.
			var at *activeToolCall
			if tc.Index != nil {
				at = toolsByIndex[*tc.Index]
			}
			if at == nil && tc.ID != "" {
				at = toolsByID[tc.ID]
			}
			if at == nil {
				contentIdx := len(partial.Content)
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall",
					ID:   tc.ID,
					Name: name,
				})
				activeTools = append(activeTools, &activeToolCall{
					id:         tc.ID,
					name:       name,
					custom:     isCustom,
					contentIdx: contentIdx,
				})
				at = activeTools[len(activeTools)-1]
				send(&goai.ToolCallStartEvent{ContentIndex: contentIdx, Partial: partial})
			}
			if tc.Index != nil && !at.indexed {
				at.indexed = true
				toolsByIndex[*tc.Index] = at
			}
			if tc.ID != "" {
				// Keep later IDs as aliases without replacing the call's ID.
				toolsByID[tc.ID] = at
				if at.id == "" {
					at.id = tc.ID
					partial.Content[at.contentIdx].ID = tc.ID
				}
			}
			if at.name == "" && name != "" {
				at.name = name
				partial.Content[at.contentIdx].Name = name
			}
			if isCustom {
				at.custom = true
			}

			// Function streams emit a delta for each frame, even when the
			// argument fragment is empty (usually on the first frame).
			if string(tc.Function.Arguments) != "" || !at.custom {
				partial.Content[at.contentIdx].SetStreamingArguments(at.argumentStream.Append(string(tc.Function.Arguments)), "")
				send(&goai.ToolCallDeltaEvent{ContentIndex: at.contentIdx, Delta: string(tc.Function.Arguments), Partial: partial})
			}
			if tc.Custom.Input != "" {
				property, selected := grammarInputPropertyFor(at.name, toolSets)
				if !selected {
					property = "input"
				}
				at.customStream.Property = property
				at.customStream.PropertySet = true
				deltaJSON, err := at.customStream.Append(string(tc.Custom.Input))
				if err != nil {
					failStream(err)
					return
				}
				partial.Content[at.contentIdx].SetStreamingArguments(at.customStream.Arguments(), "")
				if deltaJSON != "" {
					send(&goai.ToolCallDeltaEvent{ContentIndex: at.contentIdx, Delta: deltaJSON, Partial: partial})
				}
			}
		}

	}

	applyReasoningReplayData()

	// Close any open text blocks
	for i, c := range partial.Content {
		if c.Type == "text" {
			send(&goai.TextEndEvent{ContentIndex: i, Content: c.Text, Partial: partial})
		}
		if c.Type == "thinking" {
			send(&goai.ThinkingEndEvent{ContentIndex: i, Content: c.Thinking, Partial: partial})
		}
	}

	// Close tool calls and parse arguments
	for _, at := range activeTools {
		var args map[string]interface{}
		var argumentsError string
		if at.custom {
			property, selected := grammarInputPropertyFor(at.name, toolSets)
			if !selected {
				property = "input"
			}
			at.customStream.Property = property
			at.customStream.PropertySet = true
			deltaJSON, err := at.customStream.Close(nil)
			if err != nil {
				send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err})
				return
			}
			partial.Content[at.contentIdx].SetStreamingArguments(at.customStream.Arguments(), "")
			if deltaJSON != "" {
				send(&goai.ToolCallDeltaEvent{ContentIndex: at.contentIdx, Delta: deltaJSON, Partial: partial})
			}
			args = at.customStream.Arguments()
		} else {
			args, argumentsError = at.argumentStream.Finish()
			if args == nil {
				args = map[string]interface{}{}
			}
		}
		partial.Content[at.contentIdx].Arguments = args
		partial.Content[at.contentIdx].ArgumentsError = argumentsError
		partial.Content[at.contentIdx].SetStreamingArguments(args, argumentsError)
		call := goai.ToolCall{Type: "toolCall", ID: at.id, Name: at.name, Arguments: args, ArgumentsError: argumentsError}
		call.SetStreamingArguments(args, argumentsError)
		send(goai.SnapshotEvent(&goai.ToolCallEndEvent{
			ContentIndex: at.contentIdx,
			ToolCall:     call,
			Partial:      partial,
		}))
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
	compat := goai.DetectCompatForModel(model)
	if finishReason == nil {
		if compat.SupportsFinishReason != nil && !*compat.SupportsFinishReason {
			for _, block := range partial.Content {
				if block.Type == "toolCall" {
					reason = goai.StopReasonToolUse
					break
				}
			}
		} else {
			partial.StopReason = goai.StopReasonError
			partial.ErrorMessage = "Stream ended without finish_reason"
			send(&goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: errors.New(partial.ErrorMessage)})
			return
		}
	}
	partial.StopReason = reason
	if finishReason != nil && nativeAtFinish != "" && nativeAtFinish != *finishReason {
		partial.RawStopReason = *finishReason + "." + nativeAtFinish
		if partial.StopReason == goai.StopReasonError && partial.ErrorMessage != "" {
			partial.ErrorMessage += " (native_finish_reason: " + nativeAtFinish + ")"
		}
	}
	if nativeReason != "" {
		flagOpenRouterEmptyStop(partial, nativeReason)
	}

	send(&goai.DoneEvent{Reason: partial.StopReason, Message: partial})
}

func flagOpenRouterEmptyStop(message *goai.Message, nativeReason string) {
	if message.StopReason != goai.StopReasonStop {
		return
	}
	switch strings.ToLower(nativeReason) {
	case "stop", "end_turn", "stop_sequence", "eos", "complete", "completed", "pause_turn", "compaction", "end_of_stream":
		return
	}
	// A reasoning block does not make an otherwise failed turn usable.
	for _, block := range message.Content {
		if block.Type == "toolCall" || (block.Type == "text" && strings.TrimSpace(block.Text) != "") {
			return
		}
	}
	message.StopReason = goai.StopReasonError
	message.ErrorMessage = "OpenRouter returned an empty response (native_finish_reason: " + nativeReason + ")"
}

func applyUsage(usage *goai.Usage, raw *sseUsage, model *goai.Model) {
	if usage == nil || raw == nil {
		return
	}
	cacheRead := 0
	if raw.CachedTokens != nil {
		cacheRead = *raw.CachedTokens
	}
	if raw.PromptCacheHitTokens != nil {
		cacheRead = *raw.PromptCacheHitTokens
	}
	cacheWrite := 0
	if raw.PromptTokensDetails != nil {
		if raw.PromptTokensDetails.CachedTokens != nil {
			cacheRead = *raw.PromptTokensDetails.CachedTokens
		}
		cacheWrite = raw.PromptTokensDetails.CacheWriteTokens
	}
	// Cache reads and writes are independent counts, including explicit zero.
	input := raw.PromptTokens - cacheRead - cacheWrite
	if input < 0 {
		input = 0
	}
	usage.Input = input
	usage.Output = raw.CompletionTokens
	usage.CacheRead = cacheRead
	usage.CacheWrite = cacheWrite
	usage.TotalTokens = input + raw.CompletionTokens + cacheRead + cacheWrite
	usage.CacheWrite1h = 0
	usage.CacheWrite1hSet = false
	usage.Reasoning = 0
	usage.ReasoningSet = true // pinned upstream always reports this property
	if raw.CompletionTokensDetails != nil {
		usage.Reasoning = raw.CompletionTokensDetails.ReasoningTokens
	}
	if usage.Reasoning == 0 {
		usage.Reasoning = raw.ReasoningTokens
	}
	computeCosts(usage, model)
	if model != nil && openroutercost.IsModel(string(model.Provider), model.BaseURL) {
		if total, ok := openroutercost.BilledTotal(raw.Accounting); ok {
			usage.Cost.Total = total
			usage.Cost.TotalFromProvider = &ok
		}
	}
}

func computeCosts(usage *goai.Usage, model *goai.Model) {
	usage.Cost = goai.CalculateCost(model, usage)
}
