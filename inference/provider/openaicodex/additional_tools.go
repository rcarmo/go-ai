package openaicodex

import (
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
)

func codexAnchorsTools(model *goai.Model) bool {
	c := model.ResponsesCompat
	return c != nil && c.SupportsMidConvoSystemMessages != nil && *c.SupportsMidConvoSystemMessages && c.SupportsAdditionalTools != nil && *c.SupportsAdditionalTools
}

type codexAdditionalToolState struct {
	state responsestream.SystemToolState
}

func (s *codexAdditionalToolState) systemItems(model *goai.Model, message goai.Message, index, folded int) []interface{} {
	mode := "none"
	if codexAnchorsTools(model) {
		mode = "changes"
	} else if codexUsesToolSearch(model) {
		mode = "additions"
	}
	s.state.Mode = mode
	s.state.Folded = folded
	role := "system"
	if model.Reasoning {
		role = "developer"
	}
	return s.state.SystemItems(message, role, fmt.Sprintf("system:%d", index), codexSystemText(message, true), func(defs []goai.Tool, deferLoading bool) any {
		tools := buildCodexToolDeclarations(model, defs)
		if deferLoading {
			for i := range tools {
				yes := true
				tools[i].DeferLoading = &yes
			}
		}
		return tools
	})
}
