package openaicodex

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
	"strings"
)

func codexSystemText(message goai.Message, update bool) string {
	return responsestream.SystemMessageText(message, update)
}
func codexTranscriptContext(model *goai.Model, input *goai.Context) *goai.Context {
	normalized := goai.NormalizeContext(input)
	// Legacy whitespace-only prompts are meaningful instructions too.
	if input != nil && input.SystemPrompt != "" && strings.TrimSpace(input.SystemPrompt) == "" {
		content := []goai.ContentBlock{{Type: "text", Text: input.SystemPrompt}}
		if len(input.Tools) > 0 {
			normalized.Messages[0].Content = content
		} else {
			normalized.Messages = append([]goai.Message{{Role: goai.RoleSystem, Content: content}}, normalized.Messages...)
		}
	}
	result := &goai.Context{Tools: goai.ContextFromTranscript(normalized).Tools}
	supportsMid := model.ResponsesCompat != nil && model.ResponsesCompat.SupportsMidConvoSystemMessages != nil && *model.ResponsesCompat.SupportsMidConvoSystemMessages
	if supportsMid {
		result.Messages = normalized.Messages
		if len(result.Messages) > 0 && result.Messages[0].Role == goai.RoleSystem {
			result.SystemPrompt = codexSystemText(result.Messages[0], false)
			if codexAnchorsTools(model) || codexUsesToolSearch(model) {
				result.Messages = append([]goai.Message(nil), result.Messages...)
				result.Messages[0].Content = nil
				result.Messages[0].Sections = nil
				result.Messages[0].ToolsRemoved = nil
			} else {
				result.Messages = result.Messages[1:]
			}
		}
	} else {
		var texts []string
		sections := map[string]*string{}
		for _, message := range normalized.Messages {
			if message.Role != goai.RoleSystem {
				result.Messages = append(result.Messages, message)
				continue
			}
			if text := responsestream.SystemContent(message); text != "" {
				texts = append(texts, text)
			}
			for key, value := range message.Sections {
				if value == nil {
					delete(sections, key)
				} else {
					sections[key] = value
				}
			}
		}
		result.SystemPrompt = codexSystemText(goai.Message{Content: []goai.ContentBlock{{Type: "text", Text: strings.Join(texts, "\n\n")}}, Sections: sections}, false)
	}
	if codexUsesToolSearch(model) {
		result.Tools, _ = codexToolSearchPlan(normalized.Messages)
	}
	if result.SystemPrompt == "" {
		result.SystemPrompt = "You are a helpful assistant."
	}
	return result
}

// Keep the existing full snapshot when mid-system messages are disabled: pinned
// request/converter plans otherwise disagree and omit later tools. See evidence.
func codexUsesToolSearch(model *goai.Model) bool {
	c := model.ResponsesCompat
	return !codexAnchorsTools(model) && c != nil && c.SupportsMidConvoSystemMessages != nil && *c.SupportsMidConvoSystemMessages && c.SupportsToolSearch != nil && *c.SupportsToolSearch
}

func codexToolSearchPlan(messages []goai.Message) ([]goai.Tool, int) {
	return responsestream.ToolSearchPlan(messages)
}
