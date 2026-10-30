package openaicodex

import (
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
	"strings"
)

func codexAnchorsTools(model *goai.Model) bool {
	c := model.ResponsesCompat
	return c != nil && c.SupportsMidConvoSystemMessages != nil && *c.SupportsMidConvoSystemMessages && c.SupportsAdditionalTools != nil && *c.SupportsAdditionalTools
}

const codexIncrementalToolsNotice = "This is an incremental tools update. Previously declared tools remain available unless explicitly marked unavailable. A tool declared again here is available again, and its latest definition replaces the earlier one."

func codexToolRemovalNotice(names []string) string {
	return "The following tools are no longer available. Do not call them:\n- " + strings.Join(names, "\n- ")
}

type codexAdditionalToolState struct {
	declared    bool
	unavailable []string
	systemIndex int
}

func (s *codexAdditionalToolState) systemItems(model *goai.Model, message goai.Message, messageIndex, folded int) []interface{} {
	var items []interface{}
	role := "system"
	if model.Reasoning {
		role = "developer"
	}
	text := codexSystemText(message, true)
	search := codexUsesToolSearch(model)
	anchors := codexAnchorsTools(model) || search && s.systemIndex >= folded
	s.systemIndex++
	if anchors {
		added := map[string]bool{}
		for _, tool := range message.ToolsAdded {
			added[tool.Name] = true
		}
		remaining := s.unavailable[:0]
		for _, name := range s.unavailable {
			if !added[name] {
				remaining = append(remaining, name)
			}
		}
		s.unavailable = remaining
		if len(message.ToolsAdded) > 0 {
			if s.declared && !search {
				notice := codexIncrementalToolsNotice
				if len(s.unavailable) > 0 {
					notice += "\nStill unavailable: " + strings.Join(s.unavailable, ", ") + "."
				}
				items = append(items, map[string]interface{}{"role": role, "content": notice})
			}
			s.declared = true
			tools := buildCodexToolDeclarations(model, message.ToolsAdded)
			if search {
				names := make([]string, len(message.ToolsAdded))
				for i, tool := range message.ToolsAdded {
					names[i] = tool.Name
				}
				callID := responsestream.ToolLoadID(fmt.Sprintf("system:%d", messageIndex), names)
				for i := range tools {
					deferLoading := true
					tools[i].DeferLoading = &deferLoading
				}
				items = append(items, map[string]interface{}{"type": "tool_search_call", "call_id": callID, "execution": "client", "status": "completed", "arguments": map[string]interface{}{"query": strings.Join(names, " "), "limit": len(names)}}, map[string]interface{}{"type": "tool_search_output", "call_id": callID, "execution": "client", "status": "completed", "tools": tools})
			} else {
				items = append(items, map[string]interface{}{"type": "additional_tools", "role": "developer", "tools": tools})
			}
		}
		var removed []string
		for _, tool := range message.ToolsRemoved {
			if !added[tool.Name] {
				removed = append(removed, tool.Name)
			}
		}
		if len(removed) > 0 {
			notice := codexToolRemovalNotice(removed)
			if text != "" {
				text += "\n\n"
			}
			text += notice
			for _, name := range removed {
				found := false
				for _, old := range s.unavailable {
					if old == name {
						found = true
						break
					}
				}
				if !found {
					s.unavailable = append(s.unavailable, name)
				}
			}
		}
	}
	if text != "" {
		items = append(items, map[string]interface{}{"role": role, "content": goai.SanitizeSurrogates(text)})
	}
	return items
}
