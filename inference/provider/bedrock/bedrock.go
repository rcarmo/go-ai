// Package bedrock implements the Amazon Bedrock ConverseStream API provider.
//
// Uses the AWS SDK v2 for Go with SigV4 signing. Supports Claude, Nova, Mistral,
// and other models hosted on Bedrock. Handles thinking/reasoning, tool calling,
// prompt caching, and image content.
package bedrock

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockdoc "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	bearer "github.com/aws/smithy-go/auth/bearer"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func init() {
	goai.RegisterApi(&goai.ApiProvider{
		Api:          goai.ApiBedrockConverseStream,
		Stream:       streamBedrock,
		StreamSimple: streamBedrockSimple,
	})
}

func streamBedrockSimple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	return streamBedrock(ctx, model, convCtx, opts)
}

func streamBedrock(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
	ch := make(chan goai.Event, 32)

	go func() {
		defer close(ch)

		goai.GetLogger().Debug("stream start", "api", "bedrock-converse-stream", "provider", model.Provider, "model", model.ID)

		// Resolve region/config, matching upstream's precedence: ARN-embedded >
		// scoped/process env > explicit endpoint region when pinned > us-east-1.
		env := goai.ProviderEnvFromOptions(opts)
		configuredRegion := getConfiguredBedrockRegion(model, opts, env)
		ambientProfile := goai.GetProviderEnvValue("AWS_PROFILE", nil) != ""
		endpointRegion := getStandardBedrockEndpointRegion(model.BaseURL)
		useExplicitEndpoint := shouldUseExplicitBedrockEndpoint(model.BaseURL, configuredRegion, ambientProfile)
		region := bedrockARNRegion(model.ID)
		if region == "" {
			region = configuredRegion
		}
		if region == "" && endpointRegion != "" && useExplicitEndpoint {
			region = endpointRegion
		}
		if region == "" {
			region = "us-east-1"
		}

		loadOpts := []func(*config.LoadOptions) error{config.WithRegion(region)}
		profile := goai.GetProviderEnvValue("AWS_PROFILE", env)
		if opts != nil && opts.Profile != "" {
			profile = opts.Profile
		}
		if profile != "" {
			loadOpts = append(loadOpts, config.WithSharedConfigProfile(profile))
		}
		if creds := getConfiguredBedrockCredentials(env); creds != nil {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(creds))
		}
		if goai.GetProviderEnvValue("AWS_BEDROCK_SKIP_AUTH", env) == "1" {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("dummy-access-key", "dummy-secret-key", "")))
		}

		// Load AWS config
		awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
		if err != nil {
			goai.GetLogger().Warn("AWS config error", "error", err)
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("AWS config: %w", err)}
			return
		}

		// Create client
		clientOpts := []func(*bedrockruntime.Options){}
		if model.BaseURL != "" && useExplicitEndpoint {
			clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
				o.BaseEndpoint = aws.String(model.BaseURL)
			})
		}
		token := goai.GetProviderEnvValue("AWS_BEARER_TOKEN_BEDROCK", env)
		if opts != nil && opts.BearerToken != "" {
			token = opts.BearerToken
		}
		if token != "" && goai.GetProviderEnvValue("AWS_BEDROCK_SKIP_AUTH", env) != "1" {
			clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
				o.BearerAuthTokenProvider = bearer.StaticTokenProvider{Token: bearer.Token{Value: token}}
			})
		}
		if goai.GetProviderEnvValue("AWS_BEDROCK_FORCE_HTTP1", env) == "1" {
			clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
				o.HTTPClient = &http.Client{Transport: &http.Transport{ForceAttemptHTTP2: false}}
			})
		}
		if opts != nil && (len(opts.Headers) > 0 || len(opts.SuppressHeaders) > 0) {
			headers := cloneStringMap(opts.Headers)
			suppress := append([]string(nil), opts.SuppressHeaders...)
			clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
				o.APIOptions = append(o.APIOptions, addCustomHeadersMiddleware(headers, suppress))
			})
		}
		client := bedrockruntime.NewFromConfig(awsCfg, clientOpts...)

		// Build request
		input := buildConverseInput(model, convCtx, opts)
		payload, err := goai.InvokeOnPayload(opts, input, model)
		if err != nil {
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: err}
			return
		}
		if replaced, ok := payload.(*bedrockruntime.ConverseStreamInput); ok && replaced != nil {
			input = replaced
		}

		// Send
		resp, err := client.ConverseStream(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonAborted, Err: ctx.Err()}
			} else {
				goai.GetLogger().Warn("Bedrock API error", "provider", model.Provider, "model", model.ID, "error", err)
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("bedrock: %w", err)}
			}
			return
		}

		processConverseStream(resp, model, ch)
	}()

	return ch
}

var standardBedrockEndpointRe = regexp.MustCompile(`^bedrock-runtime(?:-fips)?\.([a-z0-9-]+)\.amazonaws\.com(?:\.cn)?$`)

func getConfiguredBedrockRegion(model *goai.Model, opts *goai.StreamOptions, env goai.ProviderEnv) string {
	if r := bedrockARNRegion(model.ID); r != "" {
		return r
	}
	if opts != nil && opts.Region != "" {
		return opts.Region
	}
	if r := goai.GetProviderEnvValue("AWS_REGION", env); r != "" {
		return r
	}
	return goai.GetProviderEnvValue("AWS_DEFAULT_REGION", env)
}

func getConfiguredBedrockCredentials(env goai.ProviderEnv) aws.CredentialsProvider {
	accessKeyID := goai.GetProviderEnvValue("AWS_ACCESS_KEY_ID", env)
	secretAccessKey := goai.GetProviderEnvValue("AWS_SECRET_ACCESS_KEY", env)
	if accessKeyID == "" || secretAccessKey == "" {
		return nil
	}
	sessionToken := goai.GetProviderEnvValue("AWS_SESSION_TOKEN", env)
	return credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, sessionToken)
}

func bedrockARNRegion(modelID string) string {
	parts := strings.Split(modelID, ":")
	if len(parts) >= 4 && strings.HasPrefix(parts[0], "arn") && strings.HasPrefix(parts[2], "bedrock") {
		return parts[3]
	}
	return ""
}

func extractRegionFromURL(baseURL string) string {
	return getStandardBedrockEndpointRegion(baseURL)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func isReservedBedrockHeader(key string) bool {
	lower := strings.ToLower(key)
	return lower == "authorization" || lower == "host" || strings.HasPrefix(lower, "x-amz-")
}

func addCustomHeadersMiddleware(headers map[string]string, suppressHeaders []string) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Build.Add(middleware.BuildMiddlewareFunc("go-ai-custom-headers", func(ctx context.Context, in middleware.BuildInput, next middleware.BuildHandler) (out middleware.BuildOutput, metadata middleware.Metadata, err error) {
			if req, ok := in.Request.(*smithyhttp.Request); ok && req != nil {
				for k, v := range headers {
					if !isReservedBedrockHeader(k) {
						req.Header.Set(k, v)
					}
				}
				for _, name := range suppressHeaders {
					if !isReservedBedrockHeader(name) {
						req.Header.Del(name)
					}
				}
			}
			return next.HandleBuild(ctx, in)
		}), middleware.After)
	}
}

func getStandardBedrockEndpointRegion(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	m := standardBedrockEndpointRe.FindStringSubmatch(strings.ToLower(u.Hostname()))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func shouldUseExplicitBedrockEndpoint(baseURL, configuredRegion string, hasAmbientConfiguredProfile bool) bool {
	endpointRegion := getStandardBedrockEndpointRegion(baseURL)
	if endpointRegion == "" {
		return baseURL != ""
	}
	return configuredRegion == "" && !hasAmbientConfiguredProfile
}

// isGovCloudBedrockTarget checks if the model targets a GovCloud region.
func isGovCloudBedrockTarget(model *goai.Model, opts *goai.StreamOptions, env goai.ProviderEnv) bool {
	region := getConfiguredBedrockRegion(model, opts, env)
	if strings.HasPrefix(strings.ToLower(region), "us-gov-") {
		return true
	}
	id := strings.ToLower(model.ID)
	return strings.HasPrefix(id, "us-gov.") || strings.HasPrefix(id, "arn:aws-us-gov:")
}

// --- Request building ---

func buildConverseInput(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) *bedrockruntime.ConverseStreamInput {
	input := &bedrockruntime.ConverseStreamInput{
		ModelId: aws.String(model.ID),
	}

	// Resolve cache retention
	cacheRetention := ""
	if opts != nil && opts.CacheRetention != "" {
		cacheRetention = string(opts.CacheRetention)
	}
	if cacheRetention == "" {
		if goai.GetProviderEnvValue("PI_CACHE_RETENTION", goai.ProviderEnvFromOptions(opts)) == "long" {
			cacheRetention = "long"
		} else {
			cacheRetention = "short"
		}
	}

	// System prompt
	if convCtx.SystemPrompt != "" {
		input.System = []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{
				Value: goai.SanitizeSurrogates(convCtx.SystemPrompt),
			},
		}
		if cacheRetention != "none" && supportsPromptCaching(model, goai.ProviderEnvFromOptions(opts)) {
			cp := types.CachePointBlock{Type: types.CachePointTypeDefault}
			if cacheRetention == "long" {
				cp.Ttl = types.CacheTTLOneHour
			}
			input.System = append(input.System, &types.SystemContentBlockMemberCachePoint{
				Value: cp,
			})
		}
	}

	// Messages
	input.Messages = convertMessages(convCtx, model, cacheRetention, goai.ProviderEnvFromOptions(opts))

	// Inference config
	inferenceConfig := &types.InferenceConfiguration{}
	hasConfig := false
	if opts != nil && opts.MaxTokens != nil {
		inferenceConfig.MaxTokens = aws.Int32(int32(*opts.MaxTokens))
		hasConfig = true
	}
	if opts != nil && opts.Temperature != nil {
		temp := float32(*opts.Temperature)
		inferenceConfig.Temperature = &temp
		hasConfig = true
	}
	if hasConfig {
		input.InferenceConfig = inferenceConfig
	}
	if opts != nil && len(opts.RequestMetadata) > 0 {
		input.RequestMetadata = opts.RequestMetadata
	}

	// Tools
	if len(convCtx.Tools) > 0 {
		toolConfig := &types.ToolConfiguration{}
		for _, t := range convCtx.Tools {
			toolConfig.Tools = append(toolConfig.Tools, &types.ToolMemberToolSpec{
				Value: types.ToolSpecification{
					Name:        aws.String(t.Name),
					Description: aws.String(t.Description),
					InputSchema: &types.ToolInputSchemaMemberJson{
						Value: mustDocument(t.Parameters),
					},
				},
			})
		}
		input.ToolConfig = toolConfig
	}

	// Thinking config for Claude models. Newer Claude models on Bedrock support
	// adaptive thinking with native effort strings; older models use token budgets.
	if model.Reasoning && opts != nil && opts.Reasoning != nil {
		var addFields map[string]interface{}
		govCloud := isGovCloudBedrockTarget(model, opts, goai.ProviderEnvFromOptions(opts))
		if supportsAdaptiveThinking(model) {
			thinkingField := map[string]interface{}{"type": "adaptive"}
			if !govCloud {
				thinkingField["display"] = "summarized"
			}
			addFields = map[string]interface{}{
				"thinking":      thinkingField,
				"output_config": map[string]interface{}{"effort": mapThinkingLevelToEffort(model, *opts.Reasoning)},
			}
		} else {
			budget := goai.GetThinkingBudget(*opts.Reasoning, opts.ThinkingBudgets)
			thinkingField := map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": budget,
			}
			if !govCloud {
				thinkingField["display"] = "summarized"
			}
			addFields = map[string]interface{}{
				"thinking":       thinkingField,
				"anthropic_beta": []string{"interleaved-thinking-2025-05-14"},
			}
		}
		input.AdditionalModelRequestFields = mustDocument(mustJSON(addFields))
	}

	return input
}

func bedrockToolResultText(text string) string {
	text = goai.SanitizeSurrogates(text)
	if strings.TrimSpace(text) == "" {
		return "<empty>"
	}
	return text
}

func convertMessages(convCtx *goai.Context, model *goai.Model, cacheRetention string, env goai.ProviderEnv) []types.Message {
	var result []types.Message
	transformed := goai.TransformMessages(convCtx.Messages, model)

	for i := 0; i < len(transformed); i++ {
		msg := transformed[i]
		switch msg.Role {
		case goai.RoleUser:
			var content []types.ContentBlock
			for _, b := range msg.Content {
				switch b.Type {
				case "text":
					text := goai.SanitizeSurrogates(b.Text)
					if strings.TrimSpace(text) == "" {
						continue
					}
					content = append(content, &types.ContentBlockMemberText{Value: text})
				case "image":
					content = append(content, createImageBlock(b.MimeType, b.Data))
				}
			}
			if len(content) == 0 {
				content = append(content, &types.ContentBlockMemberText{Value: "<empty>"})
			}
			result = append(result, types.Message{
				Role:    types.ConversationRoleUser,
				Content: content,
			})

		case goai.RoleAssistant:
			var content []types.ContentBlock
			for _, b := range msg.Content {
				switch b.Type {
				case "text":
					text := goai.SanitizeSurrogates(b.Text)
					if strings.TrimSpace(text) == "" {
						continue
					}
					content = append(content, &types.ContentBlockMemberText{Value: text})
				case "toolCall":
					content = append(content, &types.ContentBlockMemberToolUse{
						Value: types.ToolUseBlock{
							ToolUseId: aws.String(b.ID),
							Name:      aws.String(b.Name),
							Input:     mustDocument(mustJSON(b.Arguments)),
						},
					})
				case "thinking":
					if strings.TrimSpace(b.Thinking) == "" {
						continue
					}
					// Thinking blocks are sent via reasoningContent for Claude
					if supportsThinkingSignature(model) && b.ThinkingSignature != "" {
						text := goai.SanitizeSurrogates(b.Thinking)
						sig := b.ThinkingSignature
						content = append(content, &types.ContentBlockMemberReasoningContent{
							Value: &types.ReasoningContentBlockMemberReasoningText{
								Value: types.ReasoningTextBlock{
									Text:      &text,
									Signature: &sig,
								},
							},
						})
					} else {
						content = append(content, &types.ContentBlockMemberText{
							Value: goai.SanitizeSurrogates(b.Thinking),
						})
					}
				}
			}
			if len(content) > 0 {
				result = append(result, types.Message{
					Role:    types.ConversationRoleAssistant,
					Content: content,
				})
			}

		case goai.RoleToolResult:
			// Collect consecutive tool results into one user message
			var toolResults []types.ContentBlock

			textResult := ""
			for _, b := range msg.Content {
				if b.Type == "text" {
					textResult += b.Text
				}
			}
			textResult = bedrockToolResultText(textResult)

			status := types.ToolResultStatusSuccess
			if msg.IsError {
				status = types.ToolResultStatusError
			}

			toolResults = append(toolResults, &types.ContentBlockMemberToolResult{
				Value: types.ToolResultBlock{
					ToolUseId: aws.String(msg.ToolCallID),
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberText{
							Value: goai.SanitizeSurrogates(textResult),
						},
					},
					Status: status,
				},
			})

			// Look ahead for consecutive tool results
			for i+1 < len(transformed) && transformed[i+1].Role == goai.RoleToolResult {
				i++
				next := transformed[i]
				nextText := ""
				for _, b := range next.Content {
					if b.Type == "text" {
						nextText += b.Text
					}
				}
				nextText = bedrockToolResultText(nextText)
				nextStatus := types.ToolResultStatusSuccess
				if next.IsError {
					nextStatus = types.ToolResultStatusError
				}
				toolResults = append(toolResults, &types.ContentBlockMemberToolResult{
					Value: types.ToolResultBlock{
						ToolUseId: aws.String(next.ToolCallID),
						Content: []types.ToolResultContentBlock{
							&types.ToolResultContentBlockMemberText{
								Value: goai.SanitizeSurrogates(nextText),
							},
						},
						Status: nextStatus,
					},
				})
			}

			result = append(result, types.Message{
				Role:    types.ConversationRoleUser,
				Content: toolResults,
			})
		}
	}

	// Add cache point to last user message for supported models
	if cacheRetention != "none" && supportsPromptCaching(model, env) && len(result) > 0 {
		last := &result[len(result)-1]
		if last.Role == types.ConversationRoleUser && last.Content != nil {
			cp := types.CachePointBlock{Type: types.CachePointTypeDefault}
			if cacheRetention == "long" {
				cp.Ttl = types.CacheTTLOneHour
			}
			last.Content = append(last.Content, &types.ContentBlockMemberCachePoint{Value: cp})
		}
	}

	return result
}

// getModelMatchCandidates returns lowercased and normalized variants of model ID/name
// for matching. Normalizes separators (spaces, underscores, dots, colons) to hyphens.
func getModelMatchCandidates(modelID string, modelName string) []string {
	values := []string{modelID}
	if modelName != "" {
		values = append(values, modelName)
	}
	var candidates []string
	for _, v := range values {
		lower := strings.ToLower(v)
		candidates = append(candidates, lower)
		// Normalize separators to hyphens
		normalized := strings.NewReplacer(" ", "-", "_", "-", ".", "-", ":", "-").Replace(lower)
		if normalized != lower {
			candidates = append(candidates, normalized)
		}
	}
	return candidates
}

func supportsAdaptiveThinking(model *goai.Model) bool {
	if model == nil {
		return false
	}
	for _, s := range getModelMatchCandidates(model.ID, model.Name) {
		if strings.Contains(s, "opus-4-6") || strings.Contains(s, "opus-4-7") ||
			strings.Contains(s, "opus-4-8") || strings.Contains(s, "sonnet-4-6") ||
			strings.Contains(s, "fable-5") {
			return true
		}
	}
	return false
}

func supportsNativeXhighEffort(model *goai.Model) bool {
	if model == nil {
		return false
	}
	for _, s := range getModelMatchCandidates(model.ID, model.Name) {
		if strings.Contains(s, "opus-4-7") || strings.Contains(s, "opus-4-8") ||
			strings.Contains(s, "fable-5") {
			return true
		}
	}
	return false
}

// isAnthropicClaudeModel checks if the model is a Claude model on Bedrock.
func isAnthropicClaudeModel(model *goai.Model) bool {
	if model == nil {
		return false
	}
	for _, s := range getModelMatchCandidates(model.ID, model.Name) {
		if strings.Contains(s, "claude") {
			return true
		}
	}
	return false
}

// supportsPromptCaching returns true for Bedrock Claude models that support cache points.
func supportsPromptCaching(model *goai.Model, env goai.ProviderEnv) bool {
	candidates := getModelMatchCandidates(model.ID, model.Name)
	hasClaudeRef := false
	for _, s := range candidates {
		if strings.Contains(s, "claude") {
			hasClaudeRef = true
			break
		}
	}
	if !hasClaudeRef {
		return goai.GetProviderEnvValue("AWS_BEDROCK_FORCE_CACHE", env) == "1"
	}
	// Claude 4.x models
	for _, s := range candidates {
		if strings.Contains(s, "-4-") {
			return true
		}
	}
	// Claude 3.7 Sonnet
	for _, s := range candidates {
		if strings.Contains(s, "claude-3-7-sonnet") {
			return true
		}
	}
	// Claude 3.5 Haiku
	for _, s := range candidates {
		if strings.Contains(s, "claude-3-5-haiku") {
			return true
		}
	}
	return false
}

// supportsThinkingSignature returns true for models that support reasoning signatures.
func supportsThinkingSignature(model *goai.Model) bool {
	return isAnthropicClaudeModel(model)
}

func mapThinkingLevelToEffort(model *goai.Model, level goai.ThinkingLevel) string {
	if level == goai.ThinkingXHigh && supportsNativeXhighEffort(model) {
		return "xhigh"
	}
	if mapped, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(level)); ok {
		return mapped
	}
	switch level {
	case goai.ThinkingMinimal, goai.ThinkingLow:
		return "low"
	case goai.ThinkingHigh, goai.ThinkingXHigh:
		return "high"
	default:
		return "medium"
	}
}

func createImageBlock(mimeType, data string) types.ContentBlock {
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		decoded = []byte(data)
	}

	var format types.ImageFormat
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		format = types.ImageFormatJpeg
	case "image/png":
		format = types.ImageFormatPng
	case "image/gif":
		format = types.ImageFormatGif
	case "image/webp":
		format = types.ImageFormatWebp
	default:
		format = types.ImageFormatPng
	}

	return &types.ContentBlockMemberImage{
		Value: types.ImageBlock{
			Source: &types.ImageSourceMemberBytes{Value: decoded},
			Format: format,
		},
	}
}

func mustDocument(data json.RawMessage) bedrockdoc.Interface {
	var v interface{}
	json.Unmarshal(data, &v)
	return bedrockdoc.NewLazyDocument(v)
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// --- Stream processing ---

func processConverseStream(resp *bedrockruntime.ConverseStreamOutput, model *goai.Model, ch chan<- goai.Event) {
	partial := &goai.Message{
		Role:     goai.RoleAssistant,
		Api:      model.Api,
		Provider: model.Provider,
		Model:    model.ID,
		Usage:    &goai.Usage{},
	}

	// Track content blocks by their Bedrock index
	type blockState struct {
		contentIdx  int
		partialJSON string
	}
	blockMap := map[int]*blockState{}

	stream := resp.GetStream()
	for event := range stream.Events() {
		switch e := event.(type) {
		case *types.ConverseStreamOutputMemberMessageStart:
			if e.Value.Role != types.ConversationRoleAssistant {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: fmt.Errorf("Unexpected assistant message start but got user message start instead")}
				return
			}
			ch <- &goai.StartEvent{Partial: partial}

		case *types.ConverseStreamOutputMemberContentBlockStart:
			idx := 0
			if e.Value.ContentBlockIndex != nil {
				idx = int(*e.Value.ContentBlockIndex)
			}
			start := e.Value.Start
			if start != nil {
				switch s := start.(type) {
				case *types.ContentBlockStartMemberToolUse:
					partial.Content = append(partial.Content, goai.ContentBlock{
						Type: "toolCall",
						ID:   aws.ToString(s.Value.ToolUseId),
						Name: aws.ToString(s.Value.Name),
					})
					ci := len(partial.Content) - 1
					blockMap[idx] = &blockState{contentIdx: ci}
					ch <- &goai.ToolCallStartEvent{ContentIndex: ci, Partial: partial}
				}
			}

		case *types.ConverseStreamOutputMemberContentBlockDelta:
			idx := 0
			if e.Value.ContentBlockIndex != nil {
				idx = int(*e.Value.ContentBlockIndex)
			}
			delta := e.Value.Delta

			switch d := delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				bs, ok := blockMap[idx]
				if !ok {
					// New text block
					partial.Content = append(partial.Content, goai.ContentBlock{Type: "text"})
					ci := len(partial.Content) - 1
					bs = &blockState{contentIdx: ci}
					blockMap[idx] = bs
					ch <- &goai.TextStartEvent{ContentIndex: ci, Partial: partial}
				}
				partial.Content[bs.contentIdx].Text += d.Value
				ch <- &goai.TextDeltaEvent{ContentIndex: bs.contentIdx, Delta: d.Value, Partial: partial}

			case *types.ContentBlockDeltaMemberToolUse:
				bs, ok := blockMap[idx]
				if !ok {
					continue
				}
				input := aws.ToString(d.Value.Input)
				bs.partialJSON += input
				args, _ := jsonparse.ParsePartialJSON(bs.partialJSON)
				if args != nil {
					partial.Content[bs.contentIdx].Arguments = args
				}
				ch <- &goai.ToolCallDeltaEvent{ContentIndex: bs.contentIdx, Delta: input, Partial: partial}

			case *types.ContentBlockDeltaMemberReasoningContent:
				bs, ok := blockMap[idx]
				if !ok {
					partial.Content = append(partial.Content, goai.ContentBlock{Type: "thinking"})
					ci := len(partial.Content) - 1
					bs = &blockState{contentIdx: ci}
					blockMap[idx] = bs
					ch <- &goai.ThinkingStartEvent{ContentIndex: ci, Partial: partial}
				}
				// ReasoningContentBlockDelta is a union type
				switch rc := d.Value.(type) {
				case *types.ReasoningContentBlockDeltaMemberText:
					partial.Content[bs.contentIdx].Thinking += rc.Value
					ch <- &goai.ThinkingDeltaEvent{ContentIndex: bs.contentIdx, Delta: rc.Value, Partial: partial}
				case *types.ReasoningContentBlockDeltaMemberSignature:
					partial.Content[bs.contentIdx].ThinkingSignature += rc.Value
				}
			}

		case *types.ConverseStreamOutputMemberContentBlockStop:
			idx := 0
			if e.Value.ContentBlockIndex != nil {
				idx = int(*e.Value.ContentBlockIndex)
			}
			bs, ok := blockMap[idx]
			if !ok {
				continue
			}
			ci := bs.contentIdx
			block := partial.Content[ci]
			switch block.Type {
			case "text":
				ch <- &goai.TextEndEvent{ContentIndex: ci, Content: block.Text, Partial: partial}
			case "thinking":
				ch <- &goai.ThinkingEndEvent{ContentIndex: ci, Content: block.Thinking, Partial: partial}
			case "toolCall":
				args, _ := jsonparse.ParsePartialJSON(bs.partialJSON)
				if args == nil {
					args = map[string]interface{}{}
				}
				partial.Content[ci].Arguments = args
				ch <- &goai.ToolCallEndEvent{
					ContentIndex: ci,
					ToolCall: goai.ToolCall{
						Type: "toolCall", ID: block.ID, Name: block.Name, Arguments: args,
					},
					Partial: partial,
				}
			}

		case *types.ConverseStreamOutputMemberMessageStop:
			partial.StopReason = mapStopReason(e.Value.StopReason)

		case *types.ConverseStreamOutputMemberMetadata:
			if e.Value.Usage != nil {
				u := e.Value.Usage
				partial.Usage = &goai.Usage{
					Input:       int(aws.ToInt32(u.InputTokens)),
					Output:      int(aws.ToInt32(u.OutputTokens)),
					TotalTokens: int(aws.ToInt32(u.TotalTokens)),
				}
				// Cache tokens if available
				if u.CacheReadInputTokens != nil {
					partial.Usage.CacheRead = int(*u.CacheReadInputTokens)
				}
				if u.CacheWriteInputTokens != nil {
					partial.Usage.CacheWrite = int(*u.CacheWriteInputTokens)
				}
				partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
			}
		}
	}

	if err := stream.Err(); err != nil {
		partial.StopReason = goai.StopReasonError
		partial.ErrorMessage = err.Error()
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: partial, Err: err}
		return
	}

	partial.Timestamp = time.Now().UnixMilli()
	if partial.StopReason == "" {
		partial.StopReason = goai.StopReasonStop
	}

	ch <- &goai.DoneEvent{Reason: partial.StopReason, Message: partial}
}

func mapStopReason(reason types.StopReason) goai.StopReason {
	switch reason {
	case types.StopReasonEndTurn, types.StopReasonStopSequence:
		return goai.StopReasonStop
	case types.StopReasonMaxTokens:
		return goai.StopReasonLength
	case types.StopReasonToolUse:
		return goai.StopReasonToolUse
	default:
		return goai.StopReasonError
	}
}
