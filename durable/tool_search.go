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
	return ToolRegistration{Definition: goai.Tool{Name: ToolSearchName, Description: "Find deferred tools by name or description and load them for the next call.", Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Keywords for a tool name, description or parameters."},"limit":{"type":"integer","minimum":1,"description":"Maximum tools to load; defaults to the registration limit."}},"required":["query"]}`)}, Implementation: "pi.tool_search", Version: 1, ReplaySafe: true, Callers: []string{"model"}, Execute: func(ctx context.Context, rawToolArguments any, api *ToolAPI) (ToolResult, error) {
		args := rawToolArguments.(JSON)
		query, ok := args["query"].(string)
		if !ok || strings.TrimSpace(query) == "" {
			return ToolResult{}, reject("query must be a non-empty string")
		}
		searchLimit := limit
		if requested, exists := args["limit"]; exists {
			var n float64
			switch value := requested.(type) {
			case int:
				n = float64(value)
			case int64:
				n = float64(value)
			case float64:
				n = value
			case json.Number:
				parsed, err := value.Float64()
				if err != nil {
					return ToolResult{}, reject("limit must be a positive integer")
				}
				n = parsed
			default:
				return ToolResult{}, reject("limit must be a positive integer")
			}
			if n <= 0 || n != float64(int(n)) {
				return ToolResult{}, reject("limit must be a positive integer")
			}
			if n < float64(searchLimit) {
				searchLimit = int(n)
			}
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
			docs = append(docs, SearchDocument{tool.Name, tool.Description + " " + schemaSearchText(schema)})
		}
		ranked := RankDocuments(query, docs, searchLimit)
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

// Deterministic schema traversal includes descriptions and property names,
// follows referenced local definitions, and leaves dormant definitions out of
// the search corpus. Sorting keeps ranking independent of Go map iteration.
func schemaSearchText(value any) string {
	parts := []string{}
	seen := map[string]bool{}
	root := value
	var visit func(any)
	visit = func(schema any) {
		object, ok := codemodeSchemaMap(schema)
		if !ok {
			return
		}
		if ref, ok := object["$ref"].(string); ok && !seen[ref] {
			if target, found := ResolveCodemodeSchemaRef(ref, root); found {
				seen[ref] = true
				visit(target)
			}
		}
		if text, ok := object["description"].(string); ok {
			parts = append(parts, text)
		}
		if properties, ok := codemodeSchemaMap(object["properties"]); ok {
			names := make([]string, 0, len(properties))
			for name := range properties {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				parts = append(parts, name)
				visit(properties[name])
			}
		}
		visit(object["items"])
		for _, key := range []string{"anyOf", "oneOf", "allOf"} {
			if variants, ok := object[key].([]any); ok {
				for _, variant := range variants {
					visit(variant)
				}
			}
		}
	}
	visit(value)
	return strings.Join(parts, " ")
}
