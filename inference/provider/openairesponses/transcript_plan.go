package openairesponses

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
)

type responsesTranscriptPlan struct {
	context *goai.Context
	mode    string
	folded  int
	tools   []goai.Tool
}

func usesLegacyDeferredMarkers(input *goai.Context) bool {
	if input != nil {
		for _, message := range input.Messages {
			if len(message.AddedToolNames) > 0 {
				return true
			}
		}
	}
	return false
}
func newResponsesTranscriptPlan(input *goai.Context, compat responsesCompat) *responsesTranscriptPlan {
	if usesLegacyDeferredMarkers(input) {
		return nil
	}
	messages := responsestream.NormalizeTranscript(input)
	if !compat.supportsMidConvoSystemMessages {
		messages = responsestream.CollapseSystemMessages(messages)
	}
	plan := &responsesTranscriptPlan{mode: "none"}
	if compat.supportsMidConvoSystemMessages && compat.supportsAdditionalTools {
		plan.mode = "changes"
	} else if compat.supportsMidConvoSystemMessages && compat.supportsToolSearch {
		plan.mode = "additions"
	}
	switch plan.mode {
	case "changes":
	case "additions":
		plan.tools, plan.folded = responsestream.ToolSearchPlan(messages)
	default:
		plan.tools = responsestream.CurrentTranscriptTools(messages)
	}
	plan.context = &goai.Context{Messages: messages, Tools: plan.tools}
	return plan
}
func (p *responsesTranscriptPlan) needsWireBudget() bool {
	if p.mode != "none" {
		return true
	}
	for _, message := range p.context.Messages {
		if len(message.Sections) > 0 {
			return true
		}
	}
	return false
}

func (p *responsesTranscriptPlan) declarations() []goai.Tool {
	tools := append([]goai.Tool(nil), p.tools...)
	if p.mode == "none" {
		return tools
	}
	system := 0
	for _, message := range p.context.Messages {
		if message.Role != goai.RoleSystem {
			continue
		}
		if p.mode == "changes" || system >= p.folded {
			tools = append(tools, message.ToolsAdded...)
		}
		system++
	}
	return tools
}
