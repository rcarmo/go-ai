package anthropic

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/stricttools"
)

func preferredStrictTool(tool goai.Tool, supported bool) bool {
	if tool.ConstrainedSampling == nil || tool.ConstrainedSampling.Type != "json_schema" || tool.ConstrainedSampling.Strict != "prefer" {
		return false
	}
	_, strict := anthropicToolInputSchema(tool, supported)
	return strict
}
func withoutPreferredStrictTools(input *goai.Context, supported bool) (*goai.Context, bool) {
	owned := *input
	found := false
	detach := func(tools []goai.Tool) []goai.Tool {
		out := append([]goai.Tool(nil), tools...)
		for i, tool := range out {
			if preferredStrictTool(tool, supported) {
				found = true
				tool.ConstrainedSampling = nil
				out[i] = tool
			}
		}
		return out
	}
	owned.Tools = detach(input.Tools)
	owned.Messages = append([]goai.Message(nil), input.Messages...)
	for i := range owned.Messages {
		owned.Messages[i].ToolsAdded = detach(input.Messages[i].ToolsAdded)
	}
	return &owned, found
}
func strictLimitError(body []byte) bool {
	var envelope struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Error.Type != "invalid_request_error" {
		return false
	}
	return stricttools.LimitMessage(envelope.Error.Message)
}
