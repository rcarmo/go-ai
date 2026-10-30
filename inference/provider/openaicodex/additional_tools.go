package openaicodex

import (
	goai "github.com/rcarmo/go-ai"
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
}

func (s *codexAdditionalToolState) systemItems(model *goai.Model, message goai.Message) []interface{} {
	var items []interface{}
	role := "system"
	if model.Reasoning {
		role = "developer"
	}
	text := codexSystemText(message, true)
	if codexAnchorsTools(model) {
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
			if s.declared {
				notice := codexIncrementalToolsNotice
				if len(s.unavailable) > 0 {
					notice += "\nStill unavailable: " + strings.Join(s.unavailable, ", ") + "."
				}
				items = append(items, map[string]interface{}{"role": role, "content": notice})
			}
			s.declared = true
			items = append(items, map[string]interface{}{"type": "additional_tools", "role": "developer", "tools": buildCodexToolDeclarations(model, message.ToolsAdded)})
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
