package durable

import goai "github.com/rcarmo/go-ai"

// ReplayToolDeclarations preserves declaration order, including remove/re-add.
func ReplayToolDeclarations(messages []MessageReceipt) []ContributionTool {
	shown := []ContributionTool{}
	for _, message := range messages {
		if message.Role != goai.RoleSystem {
			continue
		}
		for _, removed := range message.ToolsRemoved {
			for i, tool := range shown {
				if tool.Name == removed.Name {
					shown = append(shown[:i], shown[i+1:]...)
					break
				}
			}
		}
		for _, added := range message.ToolsAdded {
			index := -1
			for i, tool := range shown {
				if tool.Name == added.Name {
					index = i
					break
				}
			}
			if index < 0 {
				shown = append(shown, added)
			} else {
				shown[index] = added
			}
		}
	}
	return shown
}
func sameDeclaration(a, b ContributionTool) bool {
	return a.Name == b.Name && a.Description == b.Description && equalJSONValue(a.Parameters, b.Parameters)
}
func promptBaseline(view ContextView) bool {
	if view.Head == nil {
		return false
	}
	for _, entry := range view.Entries {
		if entry.Kind == "pi.system" && entry.ID > view.Head.ID {
			return false
		}
	}
	return true
}

// PlanSystemEntries augments section patches with positional tool declarations.
// Changed schemas are removed/re-added; order changes replace the full loadout.
// A post-head baseline omits retained older pi.system entries through edits.
func PlanSystemEntries(view ContextView, desired []RenderedSection, tools []goai.Tool, timestamp int64, limits Limits) ([]Entry, error) {
	drafts, err := PlanPromptEntries(view, desired, timestamp)
	if err != nil {
		return nil, err
	}
	receipt, err := contributionReceipt(goai.Message{Role: goai.RoleSystem, Content: []goai.ContentBlock{}, ToolsAdded: tools}, limits)
	if err != nil {
		return nil, err
	}
	added := []ContributionTool{}
	removed := []goai.ToolReference{}
	if promptBaseline(view) {
		added = receipt.ToolsAdded
	} else {
		offered := ReplayToolDeclarations(view.Messages)
		wanted := map[string]ContributionTool{}
		for _, tool := range receipt.ToolsAdded {
			wanted[tool.Name] = tool
		}
		kept := []ContributionTool{}
		names := map[string]bool{}
		for _, tool := range offered {
			if next, ok := wanted[tool.Name]; ok && sameDeclaration(tool, next) {
				kept = append(kept, tool)
				names[tool.Name] = true
			} else {
				removed = append(removed, goai.ToolReference{Name: tool.Name})
			}
		}
		for _, tool := range receipt.ToolsAdded {
			if !names[tool.Name] {
				added = append(added, tool)
			}
		}
		replayed := append(append([]ContributionTool{}, kept...), added...)
		reorder := len(replayed) != len(receipt.ToolsAdded)
		if !reorder {
			for i, tool := range replayed {
				if tool.Name != receipt.ToolsAdded[i].Name {
					reorder = true
					break
				}
			}
		}
		if reorder {
			removed = nil
			for _, tool := range offered {
				removed = append(removed, goai.ToolReference{Name: tool.Name})
			}
			added = receipt.ToolsAdded
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return drafts, nil
	}
	if len(drafts) == 0 {
		drafts = []Entry{{Kind: "pi.system", Value: JSON{}, Model: []MessageReceipt{{Role: goai.RoleSystem, Content: []goai.ContentBlock{}, Timestamp: timestamp}}}}
	}
	last := &drafts[len(drafts)-1].Model[0]
	last.ToolsAdded, last.ToolsRemoved = added, removed
	return drafts, nil
}
