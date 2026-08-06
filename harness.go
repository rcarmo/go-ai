// Hooks and helpers for building agent harnesses on top of go-ai.
//
// These provide the extension points needed for custom context compaction,
// retry strategies, session management, and observability.
package goai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// --- Context helpers ---

// CloneContext creates a deep copy of a Context.
// Messages, tools, and content blocks are all copied so mutations
// to the clone do not affect the original.
func CloneContext(ctx *Context) *Context {
	if ctx == nil {
		return nil
	}

	clone := &Context{
		SystemPrompt: ctx.SystemPrompt,
	}

	// Deep copy messages
	clone.Messages = make([]Message, len(ctx.Messages))
	for i, msg := range ctx.Messages {
		clone.Messages[i] = cloneMessage(msg)
	}

	// Deep copy tools
	if ctx.Tools != nil {
		clone.Tools = make([]Tool, len(ctx.Tools))
		for i, t := range ctx.Tools {
			clone.Tools[i] = Tool{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  append(json.RawMessage{}, t.Parameters...),
			}
		}
	}

	return clone
}

func cloneMessage(msg Message) Message {
	clone := msg

	// Deep copy content blocks
	if msg.Content != nil {
		clone.Content = make([]ContentBlock, len(msg.Content))
		for i, b := range msg.Content {
			clone.Content[i] = b
			clone.Content[i].Arguments = deepCopyStringAnyMap(b.Arguments)
		}
	}

	// Deep copy diagnostics
	if msg.Diagnostics != nil {
		clone.Diagnostics = make([]AssistantMessageDiagnostic, len(msg.Diagnostics))
		for i, d := range msg.Diagnostics {
			clone.Diagnostics[i] = d
			clone.Diagnostics[i].Details = deepCopyStringAnyMap(d.Details)
		}
	}

	clone.Details = deepCopyAny(msg.Details)

	// Deep copy usage
	if msg.Usage != nil {
		u := *msg.Usage
		clone.Usage = &u
	}

	return clone
}

func deepCopyStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = deepCopyAny(v)
	}
	return out
}

func deepCopyAny(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return deepCopyStringAnyMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, v := range t {
			out[i] = deepCopyAny(v)
		}
		return out
	case json.RawMessage:
		return append(json.RawMessage{}, t...)
	case []byte:
		return append([]byte{}, t...)
	default:
		return v
	}
}

// --- Context serialization ---

// SaveContext writes a Context to a JSON file.
func SaveContext(ctx *Context, path string) error {
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal context: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadContext reads a Context from a JSON file.
func LoadContext(path string) (*Context, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read context: %w", err)
	}
	var ctx Context
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, fmt.Errorf("unmarshal context: %w", err)
	}
	return &ctx, nil
}

// --- Token estimation ---

const estimatedImageChars = 4800

type ContextUsageEstimate struct {
	Tokens         int
	UsageTokens    int
	TrailingTokens int
	LastUsageIndex *int
}

// EstimateTextTokens estimates tokens using upstream pi-ai's ~4 chars/token heuristic.
func EstimateTextTokens(text string) int { return ceilDiv(len(text), 4) }

// CalculateContextTokens returns totalTokens when reported, otherwise sums usage parts.
func CalculateContextTokens(usage *Usage) int {
	if usage == nil {
		return 0
	}
	if usage.TotalTokens != 0 {
		return usage.TotalTokens
	}
	return usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}

// EstimateMessageTokens estimates tokens for one message, matching upstream utils/estimate.ts.
func EstimateMessageTokens(msg Message) int {
	chars := 0
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			chars += len(b.Text)
		case "image":
			chars += estimatedImageChars
		case "thinking":
			chars += len(b.Thinking)
		case "toolCall":
			argsJSON, _ := json.Marshal(b.Arguments)
			chars += len(b.Name) + len(argsJSON)
		}
	}
	return ceilDiv(chars, 4)
}

// EstimateContextTokens returns the upstream-style context estimate split into usage/trailing parts.
func EstimateContextTokens(ctx *Context) ContextUsageEstimate {
	if ctx == nil {
		return ContextUsageEstimate{}
	}
	latestPrefixTimestamp := int64(-1 << 63)
	lastUsageIndex := -1
	usageTokens := 0
	for i, msg := range ctx.Messages {
		if msg.Role == RoleAssistant {
			usageAppliesToPrefix := msg.Timestamp >= latestPrefixTimestamp
			if usageAppliesToPrefix && msg.StopReason != StopReasonAborted && msg.StopReason != StopReasonError {
				if tokens := CalculateContextTokens(msg.Usage); tokens > 0 {
					lastUsageIndex = i
					usageTokens = tokens
				}
			}
		}
		if msg.Timestamp > latestPrefixTimestamp {
			latestPrefixTimestamp = msg.Timestamp
		}
	}
	if lastUsageIndex >= 0 {
		trailing := 0
		for j := lastUsageIndex + 1; j < len(ctx.Messages); j++ {
			msg := ctx.Messages[j]
			trailing += EstimateMessageTokens(msg)
			trailing += estimateAddedToolDefinitions(ctx, msg)
		}
		idx := lastUsageIndex
		return ContextUsageEstimate{Tokens: usageTokens + trailing, UsageTokens: usageTokens, TrailingTokens: trailing, LastUsageIndex: &idx}
	}

	trailing := 0
	for _, msg := range ctx.Messages {
		trailing += EstimateMessageTokens(msg)
		trailing += estimateAddedToolDefinitions(ctx, msg)
	}
	prefix := EstimateTextTokens(ctx.SystemPrompt)
	if len(ctx.Tools) > 0 {
		toolsJSON, _ := json.Marshal(ctx.Tools)
		prefix += EstimateTextTokens(string(toolsJSON))
	}
	return ContextUsageEstimate{Tokens: trailing + prefix, TrailingTokens: trailing + prefix}
}

func estimateAddedToolDefinitions(ctx *Context, msg Message) int {
	if msg.Role != RoleToolResult || len(msg.AddedToolNames) == 0 || ctx == nil || len(ctx.Tools) == 0 {
		return 0
	}
	byName := map[string]Tool{}
	for _, t := range ctx.Tools {
		byName[t.Name] = t
	}
	var tools []Tool
	seen := map[string]bool{}
	for _, name := range msg.AddedToolNames {
		if t, ok := byName[name]; ok && !seen[name] {
			tools = append(tools, t)
			seen[name] = true
		}
	}
	if len(tools) == 0 {
		return 0
	}
	toolsJSON, _ := json.Marshal(tools)
	return EstimateTextTokens(string(toolsJSON))
}

// EstimateTokens provides a rough token count estimate for a context.
// Uses upstream pi-ai's estimateContextTokens heuristic.
func EstimateTokens(ctx *Context) int { return EstimateContextTokens(ctx).Tokens }

func ceilDiv(n, d int) int {
	if n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

// FitsInContextWindow checks if a context fits within a model's context window.
// Returns (fits, estimatedTokens).
func FitsInContextWindow(ctx *Context, model *Model) (bool, int) {
	tokens := EstimateTokens(ctx)
	if model == nil || model.ContextWindow <= 0 {
		return true, tokens
	}
	return tokens < model.ContextWindow, tokens
}

// --- Context compaction ---

// CompactContext removes older messages to fit within a token budget.
// It preserves the system prompt and keeps at most the most recent N messages.
//
// This is intentionally simple tail truncation. It does not summarize or
// reconstruct tool-call/tool-result pairings across the truncation boundary.
// For production use, implement a custom compaction function.
func CompactContext(ctx *Context, model *Model, keepRecent int) *Context {
	if ctx == nil {
		return nil
	}
	if keepRecent <= 0 {
		keepRecent = 10
	}

	fits, _ := FitsInContextWindow(ctx, model)
	if fits {
		return ctx
	}

	clone := CloneContext(ctx)

	// Keep at most keepRecent messages from the end
	if len(clone.Messages) > keepRecent {
		clone.Messages = clone.Messages[len(clone.Messages)-keepRecent:]
	}

	return clone
}

// --- Turn helpers ---

// AppendUserMessage adds a text user message to the context.
func AppendUserMessage(ctx *Context, text string) {
	if ctx == nil {
		return
	}
	ctx.Messages = append(ctx.Messages, UserMessage(text))
}

// AppendToolResult adds a tool result message to the context.
func AppendToolResult(ctx *Context, toolCallID, toolName, text string, isError bool) {
	if ctx == nil {
		return
	}
	ctx.Messages = append(ctx.Messages, Message{
		Role:       RoleToolResult,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    []ContentBlock{{Type: "text", Text: text}},
		IsError:    isError,
	})
}

// AppendAssistantMessage adds a completed assistant message to the context.
func AppendAssistantMessage(ctx *Context, msg *Message) {
	if ctx == nil || msg == nil {
		return
	}
	ctx.Messages = append(ctx.Messages, *msg)
}

// GetToolCalls extracts all tool calls from an assistant message.
func GetToolCalls(msg *Message) []ToolCall {
	if msg == nil {
		return nil
	}
	var calls []ToolCall
	for _, b := range msg.Content {
		if b.Type == "toolCall" {
			calls = append(calls, ToolCall{
				Type:      "toolCall",
				ID:        b.ID,
				Name:      b.Name,
				Arguments: deepCopyStringAnyMap(b.Arguments),
			})
		}
	}
	return calls
}

// GetTextContent extracts all text from a message's content blocks.
func GetTextContent(msg *Message) string {
	if msg == nil {
		return ""
	}
	var text string
	for _, b := range msg.Content {
		if b.Type == "text" {
			text += b.Text
		}
	}
	return text
}

// HasToolCalls returns true if the message contains any tool calls.
func HasToolCalls(msg *Message) bool {
	if msg == nil {
		return false
	}
	for _, b := range msg.Content {
		if b.Type == "toolCall" {
			return true
		}
	}
	return false
}

// NeedsToolExecution returns true if the message is an assistant message
// with tool calls that need to be executed before the next LLM turn.
func NeedsToolExecution(msg *Message) bool {
	if msg == nil {
		return false
	}
	return msg.Role == RoleAssistant && msg.StopReason == StopReasonToolUse && HasToolCalls(msg)
}

// --- Provider hook helpers ---

// InvokeOnPayload calls the OnPayload hook if set, returning the (possibly replaced) payload.
func InvokeOnPayload(opts *StreamOptions, payload interface{}, model *Model) (interface{}, error) {
	if opts == nil {
		return payload, nil
	}
	if opts.OnPayloadWithTelemetry != nil {
		replaced, err := opts.OnPayloadWithTelemetry(payload, model, opts.TelemetryContext)
		if err != nil {
			return nil, err
		}
		if replaced != nil {
			payload = replaced
		}
	}
	if opts.OnPayload == nil {
		return payload, nil
	}
	replaced, err := opts.OnPayload(payload, model)
	if err != nil {
		return nil, err
	}
	if replaced != nil {
		return replaced, nil
	}
	return payload, nil
}

// InvokeOnResponse calls the OnResponse hook if set.
func InvokeOnResponse(opts *StreamOptions, resp *http.Response, model *Model) {
	if opts == nil || resp == nil {
		return
	}
	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}
	if opts.OnResponseWithTelemetry != nil {
		opts.OnResponseWithTelemetry(resp.StatusCode, headers, model, opts.TelemetryContext)
	}
	if opts.OnResponse != nil {
		opts.OnResponse(resp.StatusCode, headers, model)
	}
}
