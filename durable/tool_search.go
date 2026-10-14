package durable

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	goai "github.com/rcarmo/go-ai"
)

const ToolSearchName = "tool_search"
const DefaultToolSearchLimit = 8

// ToolSearchRegistration discovers deferred model-callable tools from the
// invocation's captured agent/registry selection. Successful additions settle
// through normal ordered tool controls, so they survive replay and forks.
func ToolSearchRegistration(limit int) (ToolRegistration, error) {
	if limit == 0 {
		limit = DefaultToolSearchLimit
	}
	if limit < 1 || limit > MaxTools {
		return ToolRegistration{}, reject("invalid tool search limit")
	}
	return ToolRegistration{Definition: goai.Tool{Name: ToolSearchName, Description: "Find deferred tools by name or description and load them for the next call.", Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Keywords for a tool name, description or parameters."}},"required":["query"]}`)}, Implementation: "pi.tool_search", Version: 1, ReplaySafe: true, Callers: []string{"model"}, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
		query, ok := args["query"].(string)
		if !ok || strings.TrimSpace(query) == "" {
			return ToolResult{}, reject("query must be a non-empty string")
		}
		if api == nil || api.runtime == nil {
			return ToolResult{}, reject("tool search requires invocation")
		}
		agent, err := api.runtime.Agent(ctx)
		if err != nil {
			return ToolResult{}, err
		}
		docs := make([]SearchDocument, 0, len(agent.Deferred))
		for _, tool := range agent.Deferred {
			var schema any
			if err = json.Unmarshal(tool.Parameters, &schema); err != nil {
				return ToolResult{}, err
			}
			docs = append(docs, SearchDocument{tool.Name, tool.Name + " " + strings.ReplaceAll(tool.Name, "_", " ") + " " + tool.Description + " " + schemaSearchText(schema)})
		}
		ranked := RankDocuments(query, docs, limit)
		names := make([]string, 0, len(ranked))
		for _, match := range ranked {
			names = append(names, match.ID)
		}
		content := "No matching deferred tools found."
		var control *ToolControl
		if len(names) > 0 {
			content = "Loaded tools: " + strings.Join(names, ", ")
			control = &ToolControl{AddTools: names}
		}
		return ToolResult{Content: content, Details: JSON{"loaded": names}, Control: control}, nil
	}}, nil
}

// NewToolSearchExtension returns an installable model-only replay-safe tool.
func NewToolSearchExtension(limit int) (Extension, error) {
	tool, err := ToolSearchRegistration(limit)
	if err != nil {
		return Extension{}, err
	}
	return Extension{Name: "tool-search", Tools: []ToolRegistration{tool}}, nil
}

// Deterministic schema traversal includes descriptions and property names.
// Sorting object keys makes ranking independent of Go's map iteration order.
func schemaSearchText(value any) string {
	parts := []string{}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if text, ok := v["description"].(string); ok {
				parts = append(parts, text)
			}
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				child := v[key]
				if key == "properties" {
					if properties, ok := child.(map[string]any); ok {
						names := make([]string, 0, len(properties))
						for name := range properties {
							names = append(names, name)
						}
						sort.Strings(names)
						parts = append(parts, names...)
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(value)
	return strings.Join(parts, " ")
}
