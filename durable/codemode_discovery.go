package durable

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

func quickjsDiscoveryGlobals(start CodemodeStart) []quickjs.Global {
	globals := []quickjs.Global{}
	for _, name := range []string{"searchTools", "describeTool", "describeNamespace"} {
		name := name
		globals = append(globals, quickjs.Global{Name: name, Spread: true, Execute: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			var args []json.RawMessage
			if err := json.Unmarshal(raw, &args); err != nil || len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("invalid %s arguments", name)
			}
			var query string
			if len(args[0]) == 0 || args[0][0] != '"' {
				return nil, fmt.Errorf("%s requires a string", name)
			}
			if err := json.Unmarshal(args[0], &query); err != nil || query == "" {
				return nil, fmt.Errorf("%s requires a non-empty string", name)
			}
			switch name {
			case "describeTool":
				if len(args) != 1 {
					return nil, fmt.Errorf("invalid describeTool arguments")
				}
				for _, tool := range start.Tools {
					if tool.Name == query || tool.Identifier == query {
						return json.Marshal(RenderCodemodeToolSample(tool))
					}
				}
				return nil, fmt.Errorf("unknown tool %s", query)
			case "describeNamespace":
				if len(args) != 1 {
					return nil, fmt.Errorf("invalid describeNamespace arguments")
				}
				for _, ns := range start.Namespaces {
					if ns.Name == query || CodemodeIdentifier(ns.Name) == query {
						tools := []string{}
						for _, tool := range start.Tools {
							matched := CodemodeNamespaceOf(tool.Name, start.Namespaces)
							if matched != nil && matched.Name == ns.Name {
								tools = append(tools, tool.Identifier)
							}
						}
						return json.Marshal(JSON{"name": CodemodeIdentifier(ns.Name), "description": ns.Description, "instructions": ns.Instructions, "tools": tools})
					}
				}
				return nil, fmt.Errorf("unknown namespace %s", query)
			default:
				options := struct {
					Limit     int    `json:"limit"`
					Namespace string `json:"namespace"`
				}{Limit: DefaultToolSearchLimit}
				if len(args) == 2 {
					if len(args[1]) == 0 || args[1][0] != '{' {
						return nil, fmt.Errorf("invalid searchTools options")
					}
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(args[1], &fields); err != nil {
						return nil, fmt.Errorf("invalid searchTools options")
					}
					if raw, ok := fields["limit"]; ok {
						if string(raw) == "null" {
							return nil, fmt.Errorf("invalid searchTools limit")
						}
					}
					if raw, ok := fields["namespace"]; ok {
						if len(raw) == 0 || raw[0] != '"' {
							return nil, fmt.Errorf("invalid searchTools namespace")
						}
					}
					if err := json.Unmarshal(args[1], &options); err != nil {
						return nil, fmt.Errorf("invalid searchTools options")
					}
				}
				if options.Limit < 1 || options.Limit > 4096 {
					return nil, fmt.Errorf("invalid searchTools limit")
				}
				namespace := options.Namespace
				for _, ns := range start.Namespaces {
					if CodemodeIdentifier(ns.Name) == namespace {
						namespace = ns.Name
						break
					}
				}
				matches := SearchCodemodeTools(query, start.Tools, start.Namespaces, namespace, options.Limit)
				out := []JSON{}
				for _, match := range matches {
					for _, tool := range start.Tools {
						if tool.Name == match.ID {
							out = append(out, JSON{"name": tool.Identifier, "description": tool.Description})
							break
						}
					}
				}
				return json.Marshal(out)
			}
		}})
	}
	return globals
}

func codemodeDiscoveryName(name string) bool {
	return name == "searchTools" || name == "describeTool" || name == "describeNamespace"
}
func codemodeDiscoveryOutcome(start CodemodeStart, call CodemodeCall) (any, error) {
	raw, err := json.Marshal(call.Arguments["args"])
	if err != nil {
		return nil, err
	}
	for _, global := range quickjsDiscoveryGlobals(start) {
		if global.Name == call.Name {
			out, err := global.Execute(context.Background(), raw)
			if err != nil {
				return nil, err
			}
			var value any
			if err = decodeQuickJSValue(out, &value); err != nil {
				return nil, err
			}
			return value, nil
		}
	}
	return nil, fmt.Errorf("unknown codemode global %s", call.Name)
}
