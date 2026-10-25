package goai

import (
	"sort"
	"strings"
)

// TranscriptToolSupport describes how a transport expresses tool changes.
type TranscriptToolSupport string

const (
	// ToolSupportNone puts the current tool set in the top-level request field.
	ToolSupportNone TranscriptToolSupport = "none"
	// ToolSupportChanges declares every tool in place, so a later in-place
	// declaration can replace an earlier one and removals can be announced.
	ToolSupportChanges TranscriptToolSupport = "changes"
	// ToolSupportAdditions keeps the initial tools at the top and anchors later
	// additions at their system message.
	ToolSupportAdditions TranscriptToolSupport = "additions"
)

// FindLastNonAdditiveToolChange returns the index within systemMessages of the
// last removal or same-name redeclaration, or -1 when there is none. A name
// declared before counts as a redeclaration even after its removal: Kimi K3
// ignores a removed tool that a later tool message declares again.
func FindLastNonAdditiveToolChange(systemMessages []Message) int {
	declared := map[string]bool{}
	last := -1
	for i, msg := range systemMessages {
		if len(msg.ToolsRemoved) > 0 {
			last = i
		}
		for _, tool := range msg.ToolsAdded {
			if declared[tool.Name] {
				last = i
			}
			declared[tool.Name] = true
		}
	}
	return last
}

// Whether tool history contains a removal or same-name redeclaration that an
// addition-only transport cannot replay.
func HasNonAdditiveToolChanges(messages []Message) bool {
	return FindLastNonAdditiveToolChange(systemMessagesOf(messages)) >= 0
}

// TranscriptTools splits tool declarations between the top-level request field
// and in-place changes.
//
// With ToolSupportChanges the top-level list stays empty and every system
// message, including the leading one, declares its tools in place: an in-place
// declaration can replace an earlier in-place one, but not a top-level one.
// With ToolSupportAdditions a removal or redefinition cannot be expressed in
// place, so the top-level list becomes the tool state after the last such
// change; later additions stay anchored and the cached prefix is invalidated
// once per removal instead of on every later addition. With ToolSupportNone the
// top-level list is the current tool set.
//
// FoldedSystemMessages is the number of leading system messages already covered
// by RequestTools; later system messages anchor their own tool changes.
type TranscriptTools struct {
	RequestTools         []Tool
	FoldedSystemMessages int
}

func ResolveTranscriptTools(messages []Message, support TranscriptToolSupport) TranscriptTools {
	system := systemMessagesOf(messages)
	if support == ToolSupportNone {
		return TranscriptTools{RequestTools: currentTranscriptTools(system), FoldedSystemMessages: len(system)}
	}
	if support == ToolSupportChanges {
		return TranscriptTools{RequestTools: nil, FoldedSystemMessages: 0}
	}
	folded := FindLastNonAdditiveToolChange(system) + 1
	if len(messages) > 0 && messages[0].Role == RoleSystem && folded < 1 {
		folded = 1
	}
	return TranscriptTools{RequestTools: currentTranscriptTools(system[:folded]), FoldedSystemMessages: folded}
}

func systemMessagesOf(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == RoleSystem {
			out = append(out, msg)
		}
	}
	return out
}

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

// currentTranscriptTools is the tool state after replaying system declarations
// and removals in order.
func currentTranscriptTools(messages []Message) []Tool {
	byName := map[string]Tool{}
	order := []string{}
	for _, msg := range messages {
		if msg.Role != RoleSystem {
			continue
		}
		for _, ref := range msg.ToolsRemoved {
			delete(byName, ref.Name)
			for i, name := range order {
				if name == ref.Name {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
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
		for _, name := range sortedTranscriptSectionKeys(msg.Sections) {
			value := msg.Sections[name]
			if value == nil {
				delete(sections, name)
				for i, key := range sectionOrder {
					if key == name {
						sectionOrder = append(sectionOrder[:i], sectionOrder[i+1:]...)
						break
					}
				}
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
	var first *Message
	for i := range messages {
		if messages[i].Role == RoleSystem {
			first = &messages[i]
			break
		}
	}
	if initial := CreateInitialSystemMessage(systemText, tools); initial != nil {
		if first != nil {
			initial.Timestamp = first.Timestamp
		}
		out = append(out, *initial)
	} else if first != nil {
		out = append(out, Message{Role: RoleSystem, Content: []ContentBlock{}, Timestamp: first.Timestamp})
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
	for _, name := range sortedTranscriptSectionKeys(msg.Sections) {
		value := msg.Sections[name]
		if value == nil {
			parts = append(parts, `Removed system prompt section "`+name+`".`)
			continue
		}
		parts = append(parts, "Updated system prompt section \""+name+"\":\n\n"+*value)
	}
	return strings.Join(parts, "\n\n")
}

// Native section maps have no insertion order. Sort each patch so repeated
// provider projections are stable; cross-message replacement order is retained.
func sortedTranscriptSectionKeys(sections map[string]*string) []string {
	keys := make([]string, 0, len(sections))
	for key := range sections {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
