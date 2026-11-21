package openaicodex

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
)

func codexSystemText(message goai.Message, update bool) string {
	return responsestream.SystemMessageText(message, update)
}
func codexTranscriptContext(model *goai.Model, input *goai.Context) *goai.Context {
	normalized := goai.NormalizeContext(input)
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
		folded := goai.ResolveContext(input, false)
		result.SystemPrompt = folded.SystemPrompt
		result.Messages = folded.Messages
	}
	if codexUsesToolSearch(model) {
		result.Tools, _ = codexToolSearchPlan(normalized.Messages)
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

func codexInstructionRole(model *goai.Model) string {
	if model.Reasoning && (model.ResponsesCompat == nil || model.ResponsesCompat.SupportsDeveloperRole == nil || *model.ResponsesCompat.SupportsDeveloperRole) {
		return "developer"
	}
	return "system"
}

func codexEffort(model *goai.Model, opts *goai.StreamOptions) (string, bool) {
	if opts != nil && opts.Reasoning != nil && *opts.Reasoning != "" {
		return goai.MapThinkingLevel(model, goai.ModelThinkingLevel(*opts.Reasoning))
	}
	if !model.Reasoning {
		return "", false
	}
	if off, exists := model.ThinkingLevelMap[goai.ThinkingOff]; exists {
		if off == nil {
			return "", false
		}
		return *off, true
	}
	return "none", true
}

func codexConfigurationUpdateEffort(model *goai.Model, opts *goai.StreamOptions) string {
	if model.ResponsesCompat != nil && model.ResponsesCompat.SupportsMidConvoEffort != nil && *model.ResponsesCompat.SupportsMidConvoEffort {
		if effort, ok := codexEffort(model, opts); ok {
			return effort
		}
	}
	return ""
}

func codexMaxOutputTokens(model *goai.Model, ctx *goai.Context, opts *goai.StreamOptions) *int {
	if opts == nil || model.ResponsesCompat != nil && model.ResponsesCompat.SupportsMaxOutputTokens != nil && !*model.ResponsesCompat.SupportsMaxOutputTokens {
		return nil
	}
	output := goai.ClampStreamMaxTokens(model, ctx, opts)
	if output <= 0 {
		return nil
	}
	output = max(output, 16)
	return &output
}
