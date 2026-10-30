package openaicodex

import (
	"encoding/json"
	"fmt"
	"strings"

	goai "github.com/rcarmo/go-ai"
)

type codexToolView struct{ properties map[string]string }

func codexSupportsGrammar(model *goai.Model) bool {
	return model.ResponsesCompat != nil && model.ResponsesCompat.SupportsOpenAIGrammarTools != nil && *model.ResponsesCompat.SupportsOpenAIGrammarTools
}
func codexGrammarProperty(tool goai.Tool) (string, bool, error) {
	if tool.ConstrainedSampling == nil || tool.ConstrainedSampling.Type != "grammar" {
		return "", false, nil
	}
	variants := tool.ConstrainedSampling.Variants
	if strings.TrimSpace(variants["openai_lark"]) == "" && strings.TrimSpace(variants["openai_regex"]) == "" {
		return "", false, fmt.Errorf("tool %q cannot use grammar constrained sampling: no supported grammar variant was provided", tool.Name)
	}
	var schema struct {
		Type       string   `json:"type"`
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
		return "", false, fmt.Errorf("tool %q grammar input schema: %w", tool.Name, err)
	}
	if schema.Type != "object" || len(schema.Required) != 1 || schema.Properties[schema.Required[0]].Type != "string" {
		return "", false, fmt.Errorf("tool %q grammar input requires one required string property", tool.Name)
	}
	return schema.Required[0], true, nil
}
func newCodexToolView(model *goai.Model, context *goai.Context) (codexToolView, error) {
	view := codexToolView{properties: map[string]string{}}
	if !codexSupportsGrammar(model) || context == nil {
		return view, nil
	}
	// Validate active request definitions separately from the latest historical
	// declarations used for replay.
	for _, tool := range context.Tools {
		if _, _, err := codexGrammarProperty(tool); err != nil {
			return view, err
		}
	}
	tools := []goai.Tool{}
	indices := map[string]int{}
	for _, message := range context.Messages {
		if message.Role != goai.RoleSystem {
			continue
		}
		for _, tool := range message.ToolsAdded {
			if tool.Name == "" {
				continue
			}
			if index, ok := indices[tool.Name]; ok {
				tools[index] = tool
			} else {
				indices[tool.Name] = len(tools)
				tools = append(tools, tool)
			}
		}
	}
	for _, tool := range context.Tools {
		if _, ok := indices[tool.Name]; !ok && tool.Name != "" {
			indices[tool.Name] = len(tools)
			tools = append(tools, tool)
		}
	}
	for _, tool := range tools {
		property, selected, err := codexGrammarProperty(tool)
		if err != nil {
			return view, err
		}
		if selected {
			view.properties[tool.Name] = property
		}
	}
	for _, message := range context.Messages {
		if message.Role != goai.RoleAssistant {
			continue
		}
		for _, block := range message.Content {
			if block.Type != "toolCall" {
				continue
			}
			if property, ok := view.properties[block.Name]; ok {
				if _, valid := codexGrammarInput(block, property); !valid {
					return view, fmt.Errorf("grammar tool call %q requires string argument %q", block.Name, property)
				}
			}
		}
	}
	return view, nil
}
func (v codexToolView) inputProperty(name string) string {
	if property, ok := v.properties[name]; ok {
		return property
	}
	return "input"
}
func codexGrammarInput(block goai.ContentBlock, property string) (string, bool) {
	switch value := block.Arguments[property].(type) {
	case string:
		return value, true
	case goai.UTF16String:
		if block.HasStreamingArguments() {
			return value.WTF8(), true
		}
	}
	return "", false
}
