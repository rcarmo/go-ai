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
	urlpkg "net/url"
	"strconv"
	"strings"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/sse"
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
		var optHeaders map[string]string
		var suppressHeaders []string
		if opts != nil {
			optHeaders = opts.Headers
			suppressHeaders = opts.SuppressHeaders
		}
		if apiKey == "" && !goai.HasOpenAIAuthHeader(goai.MergeProviderHeaders(model.Headers, optHeaders, suppressHeaders)) {
			//lint:ignore ST1005 upstream pi-ai exact error string starts with a capital letter.
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("No API key for provider: %s", model.Provider)}
			return
		}

		baseURL := model.BaseURL
		requestModelID := model.ID
		var azureAPIVersion string
		if model.Api == goai.ApiAzureOpenAIResponses {
			var err error
			baseURL, requestModelID, azureAPIVersion, err = resolveAzureResponsesConfig(model, opts)
			if err != nil {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
				return
			}
		} else if goai.IsCloudflareProvider(model.Provider) {
			baseURL = goai.ResolveCloudflareBaseURL(model, goai.ProviderEnvFromOptions(opts))
		}

		body := buildRequest(model, convCtx, opts)
		body.Model = requestModelID
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

		url := strings.TrimRight(baseURL, "/") + "/responses"
		if model.Api == goai.ApiAzureOpenAIResponses && azureAPIVersion != "" {
			url += "?api-version=" + urlpkg.QueryEscape(azureAPIVersion)
		}
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
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
		if model.Api == goai.ApiAzureOpenAIResponses && opts != nil && opts.SessionID != "" {
			for k, v := range goai.AzureSessionHeaders(opts.SessionID) {
				req.Header.Set(k, v)
			}
		} else if opts != nil && opts.SessionID != "" {
			// Standard OpenAI: send session_id header if compat allows; always
			// send x-client-request-id for session affinity (matches upstream).
			compat := getResponsesCompat(model)
			if compat.sendSessionIdHeader {
				req.Header.Set("session_id", opts.SessionID)
			}
			req.Header.Set("x-client-request-id", opts.SessionID)
		}

		if opts != nil {
			goai.ApplyHeaders(req.Header, opts.Headers)
		}
		goai.ApplyDefaultHeaders(req.Header, model.Headers)
		if opts != nil {
			goai.SuppressHeaders(req.Header, opts.SuppressHeaders)
		}
		// Dynamic Copilot headers
		if model.Provider == goai.ProviderGitHubCopilot {
			for k, v := range goai.BuildCopilotDynamicHeaders(convCtx.Messages) {
				req.Header.Set(k, v)
			}
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
	ServiceTier          string           `json:"service_tier,omitempty"`
}

type reasoningConfig struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

type toolDef struct {
	Type         string                 `json:"type"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	Parameters   json.RawMessage        `json:"parameters,omitempty"`
	Strict       *bool                  `json:"strict,omitempty"`
	Format       map[string]interface{} `json:"format,omitempty"`
	DeferLoading bool                   `json:"defer_loading,omitempty"`
}

func convertResponsesTool(t goai.Tool, supportsStrictMode, supportsGrammar bool) (toolDef, error) {
	out := toolDef{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	cfg := t.ConstrainedSampling
	if cfg == nil || cfg.Type == "" || cfg.Type == "false" {
		return out, nil
	}
	switch cfg.Type {
	case "json_schema":
		if supportsStrictMode {
			b := true
			out.Strict = &b
			return out, nil
		}
		if cfg.Strict == "require" {
			return out, fmt.Errorf("tool %q requires JSON-schema constrained sampling, but strict tools are unsupported", t.Name)
		}
		return out, nil
	case "grammar":
		if !supportsGrammar {
			return out, nil
		}
		definition := strings.TrimSpace(cfg.Variants["openai_lark"])
		syntax := "lark"
		if definition == "" {
			definition = strings.TrimSpace(cfg.Variants["openai_regex"])
			syntax = "regex"
		}
		if definition == "" {
			return out, fmt.Errorf("tool %q cannot use grammar constrained sampling: no supported grammar variant was provided", t.Name)
		}
		if _, err := inferGrammarInputProperty(t); err != nil {
			return out, fmt.Errorf("tool %q cannot use grammar constrained sampling: %w", t.Name, err)
		}
		out.Type = "custom"
		out.Parameters = nil
		out.Format = map[string]interface{}{"type": "grammar", "syntax": syntax, "definition": definition}
		return out, nil
	default:
		return out, nil
	}
}

func inferGrammarInputProperty(t goai.Tool) (string, error) {
	var schema struct {
		Type       string                            `json:"type"`
		Required   []string                          `json:"required"`
		Properties map[string]map[string]interface{} `json:"properties"`
	}
	if err := json.Unmarshal(t.Parameters, &schema); err != nil {
		return "", err
	}
	if schema.Type != "object" {
		return "", fmt.Errorf("grammar constrained sampling requires an object parameter schema")
	}
	if len(schema.Required) != 1 {
		return "", fmt.Errorf("grammar constrained sampling requires exactly one required string property")
	}
	name := schema.Required[0]
	prop := schema.Properties[name]
	if prop == nil {
		return "", fmt.Errorf("grammar constrained sampling requires a properties entry for %s", name)
	}
	if prop["type"] != "string" {
		return "", fmt.Errorf("grammar constrained sampling property %s must have type string", name)
	}
	return name, nil
}

func resolveAzureResponsesConfig(model *goai.Model, opts *goai.StreamOptions) (baseURL string, deploymentName string, apiVersion string, err error) {
	env := goai.ProviderEnvFromOptions(opts)
	apiVersion = "v1"
	deploymentName = model.ID
	if opts != nil {
		if opts.AzureAPIVersion != "" {
			apiVersion = opts.AzureAPIVersion
		}
		if opts.AzureDeploymentName != "" {
			deploymentName = opts.AzureDeploymentName
		}
	}
	if v := goai.GetProviderEnvValue("AZURE_OPENAI_API_VERSION", env); apiVersion == "v1" && v != "" {
		apiVersion = v
	}
	if deploymentName == model.ID {
		if mapped := parseAzureDeploymentNameMap(goai.GetProviderEnvValue("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", env))[model.ID]; mapped != "" {
			deploymentName = mapped
		}
	}

	if opts != nil && strings.TrimSpace(opts.AzureBaseURL) != "" {
		baseURL = strings.TrimSpace(opts.AzureBaseURL)
	}
	if baseURL == "" {
		baseURL = strings.TrimSpace(goai.GetProviderEnvValue("AZURE_OPENAI_BASE_URL", env))
	}
	resourceName := ""
	if opts != nil {
		resourceName = opts.AzureResourceName
	}
	if resourceName == "" {
		resourceName = goai.GetProviderEnvValue("AZURE_OPENAI_RESOURCE_NAME", env)
	}
	if baseURL == "" && resourceName != "" {
		baseURL = "https://" + resourceName + ".openai.azure.com/openai/v1"
	}
	if baseURL == "" {
		baseURL = model.BaseURL
	}
	if baseURL == "" {
		return "", "", "", fmt.Errorf("azure OpenAI base URL is required; set AZURE_OPENAI_BASE_URL or AZURE_OPENAI_RESOURCE_NAME, or pass AzureBaseURL, AzureResourceName, or model.BaseURL")
	}
	baseURL, err = normalizeAzureBaseURL(baseURL)
	if err != nil {
		return "", "", "", err
	}
	return baseURL + "/deployments/" + urlpkg.PathEscape(deploymentName), deploymentName, apiVersion, nil
}

func parseAzureDeploymentNameMap(value string) map[string]string {
	out := map[string]string{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		modelID := strings.TrimSpace(parts[0])
		deployment := strings.TrimSpace(parts[1])
		if modelID != "" && deployment != "" {
			out[modelID] = deployment
		}
	}
	return out
}

func normalizeAzureBaseURL(baseURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := urlpkg.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid Azure OpenAI base URL: %s", baseURL)
	}
	isAzureHost := strings.HasSuffix(u.Hostname(), ".openai.azure.com") || strings.HasSuffix(u.Hostname(), ".cognitiveservices.azure.com")
	normalizedPath := strings.TrimRight(u.Path, "/")
	if isAzureHost && (normalizedPath == "" || normalizedPath == "/" || normalizedPath == "/openai") {
		u.Path = "/openai/v1"
		u.RawQuery = ""
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func buildRequest(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) responsesRequest {
	req := responsesRequest{
		Model:  model.ID,
		Stream: true,
		Store:  false,
	}

	if opts != nil {
		req.Temperature = opts.Temperature
		req.MaxOutputTokens = goai.ClampStreamMaxTokensPtr(model, convCtx, opts)
	}

	compatForDeferred := getResponsesCompat(model)
	deferredPlan := goai.PlanDeferredTools(convCtx, compatForDeferred.supportsToolSearch, false)

	// Convert messages to Responses API input format
	input := convertMessages(model, convCtx)
	if deferredPlan.HasDeferred() {
		callID := "tool_search_1"
		input = append(input, map[string]interface{}{"type": "tool_search_call", "call_id": callID, "execution": "client", "status": "completed"})
		var searched []toolDef
		for _, t := range deferredPlan.Deferred {
			searched = append(searched, toolDef{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Parameters, DeferLoading: true})
		}
		input = append(input, map[string]interface{}{"type": "tool_search_output", "call_id": callID, "execution": "client", "status": "completed", "tools": searched})
	}
	if model.Api == goai.ApiAzureOpenAIResponses {
		limited := goai.ApplyToolCallLimit(input, goai.DefaultToolCallLimitConfig())
		input = limited.Messages
	}
	inputJSON, _ := json.Marshal(input)
	req.Input = inputJSON

	// Convert tools
	activeTools := deferredPlan.Immediate
	if len(activeTools) == 0 && !deferredPlan.HasDeferred() {
		activeTools = convCtx.Tools
	}
	supportsGrammar := model.ResponsesCompat != nil && model.ResponsesCompat.SupportsOpenAIGrammarTools != nil && *model.ResponsesCompat.SupportsOpenAIGrammarTools
	for _, t := range activeTools {
		tool, err := convertResponsesTool(t, true, supportsGrammar)
		if err != nil {
			inputJSON, _ := json.Marshal([]map[string]interface{}{{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": err.Error()}}}})
			req.Input = inputJSON
			continue
		}
		req.Tools = append(req.Tools, tool)
	}

	// Reasoning — match pi-ai's format: {effort, summary} object + include encrypted content.
	if model.Reasoning {
		effort := ""
		if opts != nil && opts.Reasoning != nil {
			if mapped, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning)); ok {
				effort = mapped
			}
		}
		if effort == "" && model.Provider != goai.ProviderGitHubCopilot {
			effort = "medium"
		}
		if model.Provider == goai.ProviderGitHubCopilot && effort == "" {
			// Copilot: omit reasoning block entirely if no effort requested,
			// matching pi-ai's behavior for github-copilot without explicit effort.
		} else {
			summary := "auto"
			if opts != nil && opts.ReasoningSummary != "" {
				summary = opts.ReasoningSummary
			}
			req.Reasoning = &reasoningConfig{Effort: effort, Summary: summary}
			req.Include = []string{"reasoning.encrypted_content"}
		}
	} else if opts != nil && opts.Reasoning != nil {
		// Non-reasoning model but explicit reasoning requested — pass through if supported.
		if effort, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning)); ok {
			summary := "auto"
			if opts != nil && opts.ReasoningSummary != "" {
				summary = opts.ReasoningSummary
			}
			req.Reasoning = &reasoningConfig{Effort: effort, Summary: summary}
			req.Include = []string{"reasoning.encrypted_content"}
		}
	} else if off, ok := model.ThinkingLevelMap[goai.ThinkingOff]; ok && off != nil && model.Provider != goai.ProviderGitHubCopilot {
		req.Reasoning = &reasoningConfig{Effort: *off}
	}

	// Cache retention (compat-driven)
	compat := getResponsesCompat(model)
	cacheRetention := goai.CacheRetentionShort
	if opts != nil {
		cacheRetention = goai.ResolveCacheRetention(opts.CacheRetention, opts.Env)
	}
	if opts != nil && opts.SessionID != "" && cacheRetention != goai.CacheRetentionNone {
		req.PromptCacheKey = goai.ClampOpenAIPromptCacheKey(opts.SessionID)
	}
	if cacheRetention == goai.CacheRetentionLong && compat.supportsLongCacheRetention {
		req.PromptCacheRetention = "24h"
	}
	if opts != nil && opts.ServiceTier != "" {
		req.ServiceTier = opts.ServiceTier
	}

	return req
}

type responsesCompat struct {
	sendSessionIdHeader        bool
	supportsLongCacheRetention bool
	supportsToolSearch         bool
}

func getResponsesCompat(model *goai.Model) responsesCompat {
	c := responsesCompat{
		sendSessionIdHeader:        true,
		supportsLongCacheRetention: true,
		supportsToolSearch:         model != nil && (model.ID == "gpt-5.4" || strings.HasPrefix(model.ID, "gpt-5.4-")),
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
		if model.ResponsesCompat.SupportsToolSearch != nil {
			c.supportsToolSearch = *model.ResponsesCompat.SupportsToolSearch
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

	for msgIndex, msg := range transformed {
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
			items := buildAssistantItems(msgIndex, msg, model)
			input = append(input, items...)

		case goai.RoleToolResult:
			textResult := openAIResponsesToolResultText(msg.Content)
			callID := msg.ToolCallID
			if idx := strings.Index(callID, "|"); idx >= 0 {
				callID = callID[:idx]
			}
			callID = normalizeResponsesIDPart(callID)
			input = append(input, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  goai.SanitizeSurrogates(textResult),
			})
		}
	}

	return input
}

func openAIResponsesToolResultText(content []goai.ContentBlock) string {
	text := extractText(content)
	if strings.TrimSpace(text) == "" {
		return "(no tool output)"
	}
	return text
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

func buildAssistantItems(msgIndex int, msg goai.Message, model *goai.Model) []interface{} {
	// Check if this assistant message came from a different model variant.
	// When replaying cross-model messages, omit fc_ item IDs to avoid
	// OpenAI's reasoning/function-call pairing validation.
	isDifferentModel := msg.Model != "" && msg.Model != model.ID &&
		msg.Provider == model.Provider &&
		msg.Api == model.Api

	var items []interface{}
	textBlockIndex := 0
	for _, block := range msg.Content {
		switch block.Type {
		case "thinking":
			if block.ThinkingSignature != "" {
				// Replay the original reasoning item verbatim
				var item interface{}
				if json.Unmarshal([]byte(block.ThinkingSignature), &item) == nil {
					items = append(items, item)
				}
			} else if model.AnthropicCompat != nil && model.AnthropicCompat.AllowEmptySignature != nil && *model.AnthropicCompat.AllowEmptySignature {
				items = append(items, map[string]interface{}{
					"type":      "reasoning",
					"signature": "",
				})
			}
		case "text":
			fallbackMessageID := fmt.Sprintf("msg_pi_%d", msgIndex)
			if textBlockIndex > 0 {
				fallbackMessageID = fmt.Sprintf("msg_pi_%d_%d", msgIndex, textBlockIndex)
			}
			textBlockIndex++
			item := map[string]interface{}{
				"type":    "message",
				"id":      fallbackMessageID,
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
				if json.Unmarshal([]byte(block.TextSignature), &sig) == nil {
					msgID := sig.ID
					if msgID == "" {
						msgID = fallbackMessageID
					} else if len(msgID) > 64 {
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
			// Normalize callID for API compatibility
			callID = normalizeResponsesIDPart(callID)
			// For different-model messages, omit fc_ item IDs to avoid
			// pairing validation between reasoning and function-call items.
			if isDifferentModel && strings.HasPrefix(itemID, "fc_") {
				itemID = ""
			} else if itemID != "" {
				itemID = normalizeForeignResponsesItemID(itemID)
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

func normalizeForeignResponsesItemID(itemID string) string {
	if strings.HasPrefix(itemID, "fc_") && len(itemID) <= 64 {
		return normalizeResponsesIDPart(itemID)
	}
	return "fc_" + shortHash(itemID)
}

func shortHash(str string) string {
	h1 := uint32(0xdeadbeef)
	h2 := uint32(0x41c6ce57)
	for _, r := range str {
		ch := uint32(r)
		h1 = bitsMul(h1^ch, 2654435761)
		h2 = bitsMul(h2^ch, 1597334677)
	}
	h1 = bitsMul(h1^(h1>>16), 2246822507) ^ bitsMul(h2^(h2>>13), 3266489909)
	h2 = bitsMul(h2^(h2>>16), 2246822507) ^ bitsMul(h1^(h1>>13), 3266489909)
	return strings.ToLower(strconv.FormatUint(uint64(h2), 36) + strconv.FormatUint(uint64(h1), 36))
}

func bitsMul(a, b uint32) uint32 { return uint32(uint64(a) * uint64(b)) }

// normalizeResponsesIDPart sanitizes an ID part for the OpenAI Responses API.
// Replaces characters not matching [a-zA-Z0-9_-] with '_', truncates to 64 chars,
// and trims trailing underscores.
func normalizeResponsesIDPart(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
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
	out = strings.TrimRight(out, "_")
	return out
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
		itemType      string // "reasoning", "message", "function_call", "custom_tool_call"
		contentIdx    int
		partialJSON   string
		customPayload string
	}
	var current *activeItem
	terminalResponseSeen := false

	events := sse.Parse(body)
	for evt := range events {
		if evt.Event == sse.EventError {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("SSE stream error: %s", evt.Data)}
			return
		}
		if evt.Data == "[DONE]" {
			break
		}

		data := []byte(evt.Data)
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
			Type      string          `json:"type"`
			Item      json.RawMessage `json:"item,omitempty"`
			Response  json.RawMessage `json:"response,omitempty"`
			Delta     string          `json:"delta,omitempty"`
			Arguments string          `json:"arguments,omitempty"`
			Input     string          `json:"input,omitempty"`
			Part      json.RawMessage `json:"part,omitempty"`
			Code      string          `json:"code,omitempty"`
			Message   string          `json:"message,omitempty"`
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

			case "function_call", "custom_tool_call":
				partial.Content = append(partial.Content, goai.ContentBlock{
					Type: "toolCall",
					ID:   fmt.Sprintf("%s|%s", item.CallID, item.ID),
					Name: item.Name,
				})
				idx := len(partial.Content) - 1
				current = &activeItem{itemType: item.Type, contentIdx: idx}
				ch <- &goai.ToolCallStartEvent{ContentIndex: idx, Partial: partial}
			}

		case "response.reasoning_text.delta":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += raw.Delta
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.reasoning_summary_part.added":
			// Part initialization — no content emitted yet

		case "response.reasoning_summary_text.delta":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += raw.Delta
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.reasoning_summary_part.done":
			if current != nil && current.itemType == "reasoning" {
				partial.Content[current.contentIdx].Thinking += "\n\n"
				ch <- &goai.ThinkingDeltaEvent{ContentIndex: current.contentIdx, Delta: "\n\n", Partial: partial}
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

		case "response.custom_tool_call_input.delta":
			if current != nil && current.itemType == "custom_tool_call" {
				current.customPayload += raw.Delta
				partial.Content[current.contentIdx].Arguments = map[string]interface{}{"input": current.customPayload}
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: raw.Delta, Partial: partial}
			}

		case "response.custom_tool_call_input.done":
			if current != nil && current.itemType == "custom_tool_call" {
				current.customPayload = raw.Input
				if current.customPayload == "" {
					current.customPayload = raw.Arguments
				}
				if current.customPayload == "" {
					current.customPayload = raw.Delta
				}
				partial.Content[current.contentIdx].Arguments = map[string]interface{}{"input": current.customPayload}
			}

		case "response.function_call_arguments.done":
			if current != nil && current.itemType == "function_call" {
				// Finalize: emit any trailing delta not covered by incremental deltas
				if raw.Arguments != "" && strings.HasPrefix(raw.Arguments, current.partialJSON) {
					trailing := raw.Arguments[len(current.partialJSON):]
					if trailing != "" {
						current.partialJSON = raw.Arguments
						args, _ := jsonparse.ParsePartialJSON(current.partialJSON)
						if args != nil {
							partial.Content[current.contentIdx].Arguments = args
						}
						ch <- &goai.ToolCallDeltaEvent{ContentIndex: current.contentIdx, Delta: trailing, Partial: partial}
					}
				}
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

		case "response.completed", "response.incomplete":
			terminalResponseSeen = true
			var resp struct {
				ID     string            `json:"id"`
				Status string            `json:"status"`
				Output []json.RawMessage `json:"output"`
				Usage  *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
					TotalTokens  int `json:"total_tokens"`
					InputDetails *struct {
						CachedTokens     int `json:"cached_tokens"`
						CacheWriteTokens int `json:"cache_write_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			}
			json.Unmarshal(raw.Response, &resp)

			if resp.ID != "" {
				partial.ResponseID = resp.ID
			}
			backfillReasoningEncryptedContent(partial, resp.Output)
			if resp.Usage != nil {
				cached := 0
				cacheWrite := 0
				if resp.Usage.InputDetails != nil {
					cached = resp.Usage.InputDetails.CachedTokens
					cacheWrite = resp.Usage.InputDetails.CacheWriteTokens
				}
				input := resp.Usage.InputTokens - cached - cacheWrite
				if input < 0 {
					input = 0
				}
				partial.Usage = &goai.Usage{
					Input:       input,
					Output:      resp.Usage.OutputTokens,
					CacheRead:   cached,
					CacheWrite:  cacheWrite,
					TotalTokens: resp.Usage.TotalTokens,
				}
				partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
			}

			partial.RawStopReason = resp.Status
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
			terminalResponseSeen = true
			var resp struct {
				Status string `json:"status"`
				Error  *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
				IncompleteDetails *struct {
					Reason string `json:"reason"`
				} `json:"incomplete_details"`
			}
			json.Unmarshal(raw.Response, &resp)
			var msg string
			if resp.Error != nil {
				msg = fmt.Sprintf("%s: %s", resp.Error.Code, resp.Error.Message)
			} else if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
				msg = "incomplete: " + resp.IncompleteDetails.Reason
			} else {
				msg = "Unknown error (no error details in response)"
			}
			partial.RawStopReason = resp.Status
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("%s", msg)}
			return
		}
	}

	partial.Timestamp = time.Now().UnixMilli()
	if !terminalResponseSeen {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = "OpenAI Responses stream ended before a terminal response event"
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("OpenAI Responses stream ended before a terminal response event")}
		return
	}
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}

	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

func backfillReasoningEncryptedContent(partial *goai.Message, output []json.RawMessage) {
	if partial == nil || len(output) == 0 {
		return
	}
	byID := make(map[string]string)
	for _, raw := range output {
		var item map[string]interface{}
		if json.Unmarshal(raw, &item) != nil || item["type"] != "reasoning" {
			continue
		}
		id, _ := item["id"].(string)
		enc, _ := item["encrypted_content"].(string)
		if id != "" && enc != "" {
			byID[id] = enc
		}
	}
	if len(byID) == 0 {
		return
	}
	for i := range partial.Content {
		block := &partial.Content[i]
		if block.Type != "thinking" || block.ThinkingSignature == "" || strings.Contains(block.ThinkingSignature, "encrypted_content") {
			continue
		}
		var item map[string]interface{}
		if json.Unmarshal([]byte(block.ThinkingSignature), &item) != nil {
			continue
		}
		id, _ := item["id"].(string)
		if enc := byID[id]; enc != "" {
			item["encrypted_content"] = enc
			if b, err := json.Marshal(item); err == nil {
				block.ThinkingSignature = string(b)
			}
		}
	}
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
