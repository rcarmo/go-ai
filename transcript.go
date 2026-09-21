package goai

import "strings"

// TranscriptContext is the v0.87 normalized context shape: system prompts and
// tool declarations are represented as system messages in the transcript.
type TranscriptContext struct {
	Messages []Message `json:"messages"`
}

// CreateInitialSystemMessage returns the initial transcript system message for
// a legacy Context, or nil when both prompt and tool declarations are empty.
func CreateInitialSystemMessage(systemPrompt string, tools []Tool) *Message {
	if strings.TrimSpace(systemPrompt) == "" && len(tools) == 0 {
		return nil
	}
	msg := &Message{Role: RoleSystem, Content: []ContentBlock{{Type: "text", Text: systemPrompt}}, Timestamp: 0}
	if len(tools) > 0 {
		msg.ToolsAdded = append([]Tool{}, tools...)
	}
	return msg
}

// NormalizeContext prepends the legacy Context system prompt/tools as the
// transcript's initial system message.
func NormalizeContext(ctx *Context) TranscriptContext {
	if ctx == nil {
		return TranscriptContext{}
	}
	messages := make([]Message, 0, len(ctx.Messages)+1)
	if initial := CreateInitialSystemMessage(ctx.SystemPrompt, ctx.Tools); initial != nil {
		messages = append(messages, *initial)
	}
	messages = append(messages, ctx.Messages...)
	return TranscriptContext{Messages: messages}
}

func withoutInitialSystemMessage(messages []Message) []Message {
	if len(messages) > 0 && messages[0].Role == RoleSystem {
		return messages[1:]
	}
	return messages
}

func currentTranscriptTools(messages []Message) []Tool {
	byName := map[string]Tool{}
	order := []string{}
	for _, msg := range messages {
		if msg.Role != RoleSystem {
			continue
		}
		for _, ref := range msg.ToolsRemoved {
			delete(byName, ref.Name)
		}
		for _, tool := range msg.ToolsAdded {
			if _, ok := byName[tool.Name]; !ok {
				order = append(order, tool.Name)
			}
			byName[tool.Name] = tool
		}
	}
	out := make([]Tool, 0, len(byName))
	for _, name := range order {
		if tool, ok := byName[name]; ok {
			out = append(out, tool)
		}
	}
	return out
}

func currentTranscriptSystemText(messages []Message) string {
	var base []string
	sections := map[string]string{}
	sectionOrder := []string{}
	for _, msg := range messages {
		if msg.Role != RoleSystem {
			continue
		}
		if text := extractContentText(msg.Content); strings.TrimSpace(text) != "" {
			base = append(base, text)
		}
		for name, value := range msg.Sections {
			if value == nil {
				delete(sections, name)
				continue
			}
			if _, ok := sections[name]; !ok {
				sectionOrder = append(sectionOrder, name)
			}
			sections[name] = *value
		}
	}
	parts := append([]string{}, base...)
	for _, name := range sectionOrder {
		if value, ok := sections[name]; ok {
			parts = append(parts, "<"+name+">\n"+value+"\n</"+name+">")
		}
	}
	return strings.Join(parts, "\n\n")
}

func extractContentText(content []ContentBlock) string {
	var parts []string
	for _, block := range content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ResolveTranscript converts a legacy Context into transcript form. When
// supportsMidConvoSystemMessages is false, later system messages are folded into
// the initial system text/tools for providers that do not support native
// transcript updates.
func ResolveTranscript(ctx *Context, supportsMidConvoSystemMessages bool) TranscriptContext {
	normalized := NormalizeContext(ctx)
	if supportsMidConvoSystemMessages {
		return normalized
	}
	messages := normalized.Messages
	systemText := currentTranscriptSystemText(messages)
	tools := currentTranscriptTools(messages)
	out := make([]Message, 0, len(messages))
	if initial := CreateInitialSystemMessage(systemText, tools); initial != nil {
		out = append(out, *initial)
	}
	out = append(out, withoutInitialSystemMessage(filterNonSystemMessages(messages))...)
	return TranscriptContext{Messages: out}
}

func filterNonSystemMessages(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != RoleSystem {
			out = append(out, msg)
		}
	}
	return out
}

// ContextFromTranscript converts transcript form back to the legacy Context
// shape used by current Go provider implementations, preserving the current
// system prompt/tool snapshot and dropping system update messages from the
// replayed message slice.
func ContextFromTranscript(transcript TranscriptContext) *Context {
	return &Context{
		SystemPrompt: currentTranscriptSystemText(transcript.Messages),
		Messages:     filterNonSystemMessages(transcript.Messages),
		Tools:        currentTranscriptTools(transcript.Messages),
	}
}

// ResolveContext normalizes and resolves transcript system/tool updates for
// providers that still consume the legacy Context shape.
func ResolveContext(ctx *Context, supportsMidConvoSystemMessages bool) *Context {
	if ctx == nil {
		return &Context{}
	}
	transcript := NormalizeContext(ctx)
	if supportsMidConvoSystemMessages {
		return &Context{Messages: transcript.Messages, Tools: currentTranscriptTools(transcript.Messages)}
	}
	return ContextFromTranscript(ResolveTranscript(ctx, false))
}

// RenderSystemMessageText renders a single transcript system message for
// provider payloads. Section removals are explicit text markers so unsupported
// provider payload shapes do not silently lose removal intent.
func RenderSystemMessageText(msg Message) string {
	parts := []string{}
	if text := extractContentText(msg.Content); strings.TrimSpace(text) != "" {
		parts = append(parts, text)
	}
	for name, value := range msg.Sections {
		if value == nil {
			parts = append(parts, `Removed system prompt section "`+name+`".`)
			continue
		}
		parts = append(parts, "Updated system prompt section \""+name+"\":\n\n"+*value)
	}
	return strings.Join(parts, "\n\n")
}
