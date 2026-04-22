// Hooks and helpers for building agent harnesses on top of go-ai.
//
// These provide the extension points needed for custom context compaction,
// retry strategies, session management, and observability.
package goai

import (
	"encoding/json"
	"fmt"
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
			// Deep copy arguments map
			if b.Arguments != nil {
				clone.Content[i].Arguments = make(map[string]interface{})
				for k, v := range b.Arguments {
					clone.Content[i].Arguments[k] = v
				}
			}
		}
	}

	// Deep copy usage
	if msg.Usage != nil {
		u := *msg.Usage
		clone.Usage = &u
	}

	return clone
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

// EstimateTokens provides a rough token count estimate for a context.
// Uses the ~4 chars per token heuristic. For precise counts, use
// a provider-specific tokenizer.
func EstimateTokens(ctx *Context) int {
	total := len(ctx.SystemPrompt) / 4

	for _, msg := range ctx.Messages {
		total += 4 // message overhead
		for _, b := range msg.Content {
			switch b.Type {
			case "text":
				total += len(b.Text) / 4
			case "thinking":
				total += len(b.Thinking) / 4
			case "toolCall":
				argsJSON, _ := json.Marshal(b.Arguments)
				total += len(b.Name)/4 + len(argsJSON)/4
			case "image":
				total += 1000 // rough estimate for image tokens
			}
		}
	}

	// Tool definitions
	for _, t := range ctx.Tools {
		total += len(t.Name)/4 + len(t.Description)/4 + len(t.Parameters)/4
	}

	return total
}

// FitsInContextWindow checks if a context fits within a model's context window.
// Returns (fits, estimatedTokens).
func FitsInContextWindow(ctx *Context, model *Model) (bool, int) {
	tokens := EstimateTokens(ctx)
	return tokens < model.ContextWindow, tokens
}

// --- Context compaction ---

// CompactContext removes older messages to fit within a token budget.
// Preserves the system prompt, the most recent N messages, and all
// tool result messages that correspond to tool calls still in the context.
//
// This is a simple strategy. For production use, implement a custom
// compaction function that summarizes removed messages.
func CompactContext(ctx *Context, model *Model, keepRecent int) *Context {
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
	ctx.Messages = append(ctx.Messages, UserMessage(text))
}

// AppendToolResult adds a tool result message to the context.
func AppendToolResult(ctx *Context, toolCallID, toolName, text string, isError bool) {
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
	ctx.Messages = append(ctx.Messages, *msg)
}

// GetToolCalls extracts all tool calls from an assistant message.
func GetToolCalls(msg *Message) []ToolCall {
	var calls []ToolCall
	for _, b := range msg.Content {
		if b.Type == "toolCall" {
			calls = append(calls, ToolCall{
				Type:      "toolCall",
				ID:        b.ID,
				Name:      b.Name,
				Arguments: b.Arguments,
			})
		}
	}
	return calls
}

// GetTextContent extracts all text from a message's content blocks.
func GetTextContent(msg *Message) string {
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
	return msg.Role == RoleAssistant && msg.StopReason == StopReasonToolUse && HasToolCalls(msg)
}
