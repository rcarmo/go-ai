package responsestream

import (
	goai "github.com/rcarmo/go-ai"
	"sort"
	"strings"
)

func SystemContent(message goai.Message) string {
	var texts []string
	for _, block := range message.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// Sections are Go maps: retain the SDK's deterministic key order at projection.
func SystemMessageText(message goai.Message, update bool) string {
	var parts []string
	if text := SystemContent(message); text != "" {
		parts = append(parts, text)
	}
	keys := make([]string, 0, len(message.Sections))
	for key := range message.Sections {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := message.Sections[key]
		if update {
			if value == nil {
				parts = append(parts, "Removed system prompt section \""+key+"\".")
			} else {
				parts = append(parts, "Updated system prompt section \""+key+"\":\n\n"+*value)
			}
		} else if value != nil && *value != "" {
			parts = append(parts, *value)
		}
	}
	return strings.Join(parts, "\n\n")
}

func ToolSearchPlan(messages []goai.Message) ([]goai.Tool, int) {
	var systems []goai.Message
	seen := map[string]bool{}
	last := -1
	for _, message := range messages {
		if message.Role != goai.RoleSystem {
			continue
		}
		index := len(systems)
		systems = append(systems, message)
		if len(message.ToolsRemoved) > 0 {
			last = index
		}
		for _, tool := range message.ToolsAdded {
			if seen[tool.Name] {
				last = index
			}
			seen[tool.Name] = true
		}
	}
	folded := last + 1
	if len(messages) > 0 && messages[0].Role == goai.RoleSystem && folded < 1 {
		folded = 1
	}
	tools := goai.ContextFromTranscript(goai.TranscriptContext{Messages: systems[:folded]}).Tools
	return tools, folded
}

const IncrementalToolsNotice = "This is an incremental tools update. Previously declared tools remain available unless explicitly marked unavailable. A tool declared again here is available again, and its latest definition replaces the earlier one."

func ToolRemovalNotice(names []string) string {
	return "The following tools are no longer available. Do not call them:\n- " + strings.Join(names, "\n- ")
}

type SystemToolState struct {
	Mode        string
	Folded      int
	declared    bool
	unavailable []string
	systemIndex int
}

func (s *SystemToolState) SystemItems(message goai.Message, role, marker, text string, convert func([]goai.Tool, bool) any) []interface{} {
	var items []interface{}
	search := s.Mode == "additions"
	anchors := s.Mode == "changes" || search && s.systemIndex >= s.Folded
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
				notice := IncrementalToolsNotice
				if len(s.unavailable) > 0 {
					notice += "\nStill unavailable: " + strings.Join(s.unavailable, ", ") + "."
				}
				items = append(items, map[string]interface{}{"role": role, "content": notice})
			}
			s.declared = true
			tools := convert(message.ToolsAdded, search)
			if search {
				names := make([]string, len(message.ToolsAdded))
				for i, tool := range message.ToolsAdded {
					names[i] = tool.Name
				}
				callID := ToolLoadID(marker, names)
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
			notice := ToolRemovalNotice(removed)
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
