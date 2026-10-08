package faux

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"unicode/utf16"
)

func chars(text string) int     { return len(utf16.Encode([]rune(text))) }
func jsonText(value any) string { data, _ := json.Marshal(value); return string(data) }
func blocksText(blocks []goai.ContentBlock, assistant bool) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "thinking":
			if assistant {
				parts = append(parts, b.Thinking)
			}
		case "image":
			parts = append(parts, "image:"+b.MimeType+":"+b.Data)
		case "toolCall":
			if assistant {
				parts = append(parts, b.Name+":"+jsonText(b.Arguments))
			}
		}
	}
	return strings.Join(parts, "\n")
}
func promptParts(ctx *goai.Context) []string {
	if ctx == nil {
		return nil
	}
	parts := make([]string, 0, len(ctx.Messages)+len(ctx.Tools)+1)
	if ctx.SystemPrompt != "" {
		parts = append(parts, "system:"+ctx.SystemPrompt)
	}
	for _, m := range ctx.Messages {
		text := blocksText(m.Content, m.Role == goai.RoleAssistant)
		if m.Role == goai.RoleToolResult {
			text = m.ToolName + "\n" + text
		}
		if m.Role == goai.RoleSystem {
			for _, tool := range m.ToolsRemoved {
				text += "\ntool-:" + jsonText(tool)
			}
			for _, tool := range m.ToolsAdded {
				text += "\ntool+:" + jsonText(tool)
			}
		}
		parts = append(parts, string(m.Role)+":"+text)
	}
	for _, tool := range ctx.Tools {
		parts = append(parts, "tool:"+tool.Name+":"+tool.Description+":"+string(tool.Parameters))
	}
	return parts
}
func joinedChars(parts []string) int {
	n := max(0, (len(parts)-1)*2)
	for _, part := range parts {
		n += chars(part)
	}
	return n
}
func commonPromptChars(previous, current []string) int {
	i := 0
	for i < len(previous) && i < len(current) && previous[i] == current[i] {
		i++
	}
	prefix := joinedChars(previous[:i])
	rest := func(parts []string) string {
		if i == len(parts) {
			return ""
		}
		separator := ""
		if i > 0 {
			separator = "\n\n"
		}
		return separator + strings.Join(parts[i:], "\n\n")
	}
	left, right := utf16.Encode([]rune(rest(previous))), utf16.Encode([]rune(rest(current)))
	j := 0
	for j < len(left) && j < len(right) && left[j] == right[j] {
		j++
	}
	return prefix + j
}
func (r *Registration) estimateUsage(message *goai.Message, ctx *goai.Context, opts *goai.StreamOptions) {
	parts := promptParts(ctx)
	prompt := joinedChars(parts)
	output := (chars(blocksText(message.Content, true)) + 3) / 4
	cacheRead, cacheWrite := 0, 0
	if opts != nil && opts.SessionID != "" && opts.CacheRetention != goai.CacheRetentionNone {
		r.mu.Lock()
		if r.promptCache == nil {
			r.promptCache = map[string][]string{}
		}
		shared := commonPromptChars(r.promptCache[opts.SessionID], parts)
		cacheRead = (shared + 3) / 4
		cacheWrite = (max(0, prompt-shared) + 3) / 4
		r.promptCache[opts.SessionID] = parts
		r.mu.Unlock()
	}
	input := max(0, (prompt+3)/4-cacheRead-cacheWrite)
	message.Usage = &goai.Usage{Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite, TotalTokens: input + output + cacheRead + cacheWrite}
}
