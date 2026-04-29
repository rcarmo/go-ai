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
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	bedrockdoc "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"

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

		// Resolve region
		region := os.Getenv("AWS_REGION")
		if region == "" {
			region = os.Getenv("AWS_DEFAULT_REGION")
		}
		if region == "" {
			// Try to extract from baseUrl
			region = extractRegionFromURL(model.BaseURL)
		}
		if region == "" {
			region = "us-east-1"
		}

		// Load AWS config
		awsCfg, err := config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
		)
		if err != nil {
			goai.GetLogger().Warn("AWS config error", "error", err)
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("AWS config: %w", err)}
			return
		}

		// Create client
		clientOpts := []func(*bedrockruntime.Options){}
		if model.BaseURL != "" {
			clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
				o.BaseEndpoint = aws.String(model.BaseURL)
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
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Err: fmt.Errorf("Bedrock: %w", err)}
			}
			return
		}

		processConverseStream(resp, model, ch)
	}()

	return ch
}

func extractRegionFromURL(baseURL string) string {
	// https://bedrock-runtime.us-east-1.amazonaws.com
	lower := strings.ToLower(baseURL)
	if idx := strings.Index(lower, "bedrock-runtime."); idx >= 0 {
		rest := lower[idx+len("bedrock-runtime."):]
		if end := strings.Index(rest, ".amazonaws"); end >= 0 {
			return rest[:end]
		}
	}
	return ""
}

// --- Request building ---

func buildConverseInput(model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) *bedrockruntime.ConverseStreamInput {
	input := &bedrockruntime.ConverseStreamInput{
		ModelId: aws.String(model.ID),
	}

	// System prompt
	if convCtx.SystemPrompt != "" {
		input.System = []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{
				Value: goai.SanitizeSurrogates(convCtx.SystemPrompt),
			},
		}
	}

	// Messages
	input.Messages = convertMessages(convCtx, model)

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

	// Thinking config for Claude models
	if model.Reasoning && opts != nil && opts.Reasoning != nil {
		addFields := map[string]interface{}{
			"thinking": map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": getThinkingBudget(*opts.Reasoning, opts.ThinkingBudgets),
			},
		}
		input.AdditionalModelRequestFields = mustDocument(mustJSON(addFields))
	}

	return input
}

func convertMessages(convCtx *goai.Context, model *goai.Model) []types.Message {
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
					content = append(content, &types.ContentBlockMemberText{
						Value: goai.SanitizeSurrogates(b.Text),
					})
				case "image":
					content = append(content, createImageBlock(b.MimeType, b.Data))
				}
			}
			if len(content) > 0 {
				result = append(result, types.Message{
					Role:    types.ConversationRoleUser,
					Content: content,
				})
			}

		case goai.RoleAssistant:
			var content []types.ContentBlock
			for _, b := range msg.Content {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) == "" {
						continue
					}
					content = append(content, &types.ContentBlockMemberText{
						Value: goai.SanitizeSurrogates(b.Text),
					})
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
					// Fall back to text for non-Claude models
					if isClaudeModel(model.ID, model.Name) && b.ThinkingSignature != "" {
						// Would use reasoningContent here but the Go SDK types
						// may not support it yet. Fall back to text.
						content = append(content, &types.ContentBlockMemberText{
							Value: goai.SanitizeSurrogates(b.Thinking),
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

	return result
}

func isClaudeModel(id string, name ...string) bool {
	candidates := []string{strings.ToLower(id)}
	for _, n := range name {
		if n != "" {
			candidates = append(candidates, strings.ToLower(n))
		}
	}
	for _, s := range candidates {
		if strings.Contains(s, "anthropic.claude") || strings.Contains(s, "anthropic/claude") {
			return true
		}
	}
	return false
}

func getThinkingBudget(level goai.ThinkingLevel, custom *goai.ThinkingBudgets) int {
	if custom != nil {
		switch level {
		case goai.ThinkingMinimal:
			if custom.Minimal != nil {
				return *custom.Minimal
			}
		case goai.ThinkingLow:
			if custom.Low != nil {
				return *custom.Low
			}
		case goai.ThinkingMedium:
			if custom.Medium != nil {
				return *custom.Medium
			}
		case goai.ThinkingHigh, goai.ThinkingXHigh:
			if custom.High != nil {
				return *custom.High
			}
		}
	}
	defaults := map[goai.ThinkingLevel]int{
		goai.ThinkingMinimal: 1024,
		goai.ThinkingLow:     2048,
		goai.ThinkingMedium:  8192,
		goai.ThinkingHigh:    16384,
		goai.ThinkingXHigh:   16384,
	}
	if v, ok := defaults[level]; ok {
		return v
	}
	return 8192
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
		contentIdx int
		partialJSON string
	}
	blockMap := map[int]*blockState{}

	stream := resp.GetStream()
	for event := range stream.Events() {
		switch e := event.(type) {
		case *types.ConverseStreamOutputMemberMessageStart:
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
