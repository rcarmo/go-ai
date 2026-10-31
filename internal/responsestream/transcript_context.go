package responsestream

import (
	goai "github.com/rcarmo/go-ai"
	"strings"
)

func NormalizeTranscript(input *goai.Context) []goai.Message {
	normalized := goai.NormalizeContext(input).Messages
	if input != nil && input.SystemPrompt != "" && strings.TrimSpace(input.SystemPrompt) == "" {
		content := []goai.ContentBlock{{Type: "text", Text: input.SystemPrompt}}
		if len(input.Tools) > 0 {
			normalized[0].Content = content
		} else {
			normalized = append([]goai.Message{{Role: goai.RoleSystem, Content: content}}, normalized...)
		}
	}
	return normalized
}

func CurrentTranscriptTools(messages []goai.Message) []goai.Tool {
	return goai.ContextFromTranscript(goai.TranscriptContext{Messages: messages}).Tools
}

// CollapseSystemMessages retains section metadata until provider rendering.
func CollapseSystemMessages(messages []goai.Message) []goai.Message {
	var content []string
	sections := map[string]*string{}
	var first *goai.Message
	var rest []goai.Message
	for _, message := range messages {
		if message.Role != goai.RoleSystem {
			rest = append(rest, message)
			continue
		}
		if first == nil {
			copy := message
			first = &copy
		}
		if text := SystemContent(message); text != "" {
			content = append(content, text)
		}
		for key, value := range message.Sections {
			if value == nil {
				delete(sections, key)
			} else {
				sections[key] = value
			}
		}
	}
	if first == nil {
		return rest
	}
	initial := goai.Message{Role: goai.RoleSystem, Timestamp: first.Timestamp, Sections: sections, ToolsAdded: CurrentTranscriptTools(messages)}
	if text := strings.Join(content, "\n\n"); text != "" {
		initial.Content = []goai.ContentBlock{{Type: "text", Text: text}}
	}
	return append([]goai.Message{initial}, rest...)
}
