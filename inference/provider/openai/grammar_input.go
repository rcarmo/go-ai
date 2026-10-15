package openai

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

// Grammar custom input represents the schema's sole required string property.
// Pinned pi-ai 1.2.0 constrained-sampling.ts inferGrammarInputProperty.
func grammarInputProperty(tool goai.Tool) (string, error) {
	var schema struct {
		Type       string   `json:"type"`
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
		return "", fmt.Errorf("tool %q grammar input schema: %w", tool.Name, err)
	}
	if schema.Type != "object" || len(schema.Required) != 1 || schema.Properties[schema.Required[0]].Type != "string" {
		return "", fmt.Errorf("tool %q grammar input requires one required string property", tool.Name)
	}
	return schema.Required[0], nil
}
func grammarInputPropertyFor(name string, sets [][]goai.Tool) string {
	for _, tools := range sets {
		for _, tool := range tools {
			if tool.Name == name && tool.ConstrainedSampling != nil && tool.ConstrainedSampling.Type == "grammar" {
				property, _ := grammarInputProperty(tool)
				return property
			}
		}
	}
	return ""
}

// Historical declarations retain removed definitions and use the last schema
// for a name, matching pinned transcript.ts getDeclaredTools. Legacy context
// tools fill only names not represented in the transcript.
func grammarTranscriptTools(context *goai.Context) []goai.Tool {
	if context == nil {
		return nil
	}
	tools := []goai.Tool{}
	indices := map[string]int{}
	for _, message := range context.Messages {
		if message.Role != goai.RoleSystem {
			continue
		}
		for _, tool := range message.ToolsAdded {
			if index, ok := indices[tool.Name]; ok {
				tools[index] = tool
			} else {
				indices[tool.Name] = len(tools)
				tools = append(tools, tool)
			}
		}
	}
	for _, tool := range context.Tools {
		if _, ok := indices[tool.Name]; !ok {
			indices[tool.Name] = len(tools)
			tools = append(tools, tool)
		}
	}
	return tools
}
