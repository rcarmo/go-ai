package durable

import (
	"context"
	"sort"
)

// ToolNamespace describes an extension's tools to Codemode.
type ToolNamespace struct {
	Name         string
	Description  string
	Instructions string
}

func validateCodemodeNamespaces(namespaces []CodemodeNamespace, l Limits) error {
	if len(namespaces) > l.MaxPage {
		return reject("codemode namespace budget")
	}
	names := map[string]bool{}
	identifiers := map[string]bool{}
	prefixes := map[string]bool{}
	members := map[string]bool{}
	for _, ns := range namespaces {
		identifier := CodemodeIdentifier(ns.Name)
		if ns.Name == "" || len(ns.Name) > l.MaxStringBytes || len(ns.Prefix) > l.MaxStringBytes || len(ns.Description) > l.MaxStringBytes || len(ns.Instructions) > l.MaxStringBytes || names[ns.Name] || identifiers[identifier] {
			return reject("invalid codemode namespace")
		}
		if ns.Tools == nil {
			if ns.Prefix == "" || prefixes[ns.Prefix] {
				return reject("invalid codemode namespace prefix")
			}
			prefixes[ns.Prefix] = true
		} else {
			if ns.Prefix != "" || len(*ns.Tools) > l.MaxPage {
				return reject("invalid codemode namespace membership")
			}
			for _, name := range *ns.Tools {
				if !validToolName(name) || members[name] {
					return reject("invalid codemode namespace member")
				}
				members[name] = true
			}
		}
		names[ns.Name] = true
		identifiers[identifier] = true
	}
	return nil
}

func copyCodemodeNamespaces(in []CodemodeNamespace) []CodemodeNamespace {
	out := append([]CodemodeNamespace(nil), in...)
	for i := range out {
		if out[i].Tools != nil {
			members := append([]string{}, (*out[i].Tools)...)
			out[i].Tools = &members
		}
	}
	return out
}

// Keep the first declaration of each namespace and each tool, including tools
// first declared outside any namespace. The phase captures this alongside its
// tool offers, so prompt and execution don't resolve different selections.
type codemodeNamespaceSelection struct {
	declarations []ToolNamespace
	byTool       map[string]string
}

func captureCodemodeNamespaces(extensions []installedExtension) codemodeNamespaceSelection {
	var selection codemodeNamespaceSelection
	seen := map[string]bool{}
	for _, ext := range extensions {
		if ext.namespace != nil && !seen[ext.namespace.Name] {
			selection.declarations = append(selection.declarations, *ext.namespace)
			seen[ext.namespace.Name] = true
		}
	}
	if len(selection.declarations) == 0 {
		return selection
	}
	selection.byTool = map[string]string{}
	for _, ext := range extensions {
		for _, name := range ext.toolOrder {
			if _, exists := selection.byTool[name]; exists {
				continue
			}
			if ext.namespace == nil {
				selection.byTool[name] = ""
			} else {
				selection.byTool[name] = ext.namespace.Name
			}
		}
	}
	sort.Slice(selection.declarations, func(i, j int) bool { return selection.declarations[i].Name < selection.declarations[j].Name })
	return selection
}

func (r *TaskRuntime) codemodeNamespaces(ctx context.Context, tools []CodemodeToolDescription, fallback []CodemodeNamespace) ([]CodemodeNamespace, error) {
	resolved, err := r.phaseAgent(ctx)
	if err != nil {
		return nil, err
	}
	selection := resolved.agent.codemodeNamespaces
	if len(selection.declarations) == 0 {
		return copyCodemodeNamespaces(fallback), nil
	}
	members := map[string][]string{}
	for _, tool := range tools {
		if name := selection.byTool[tool.Name]; name != "" {
			members[name] = append(members[name], tool.Name)
		}
	}
	out := make([]CodemodeNamespace, 0, len(selection.declarations))
	for _, declaration := range selection.declarations {
		tools, ok := members[declaration.Name]
		if !ok {
			tools = []string{}
		}
		out = append(out, CodemodeNamespace{Name: declaration.Name, Description: declaration.Description, Instructions: declaration.Instructions, Tools: &tools})
	}
	return out, validateCodemodeNamespaces(out, r.harness.session.limits)
}
