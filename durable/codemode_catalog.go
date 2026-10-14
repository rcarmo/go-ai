package durable

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

const DefaultCodemodeInlineBudget = 3000

type CodemodeNamespace struct {
	Name         string
	Description  string
	Instructions string
	Prefix       string
}
type CodemodeToolDescription struct {
	Deferred     bool `json:"deferred,omitempty"`
	Name         string
	Identifier   string
	Description  string
	InputSchema  JSON
	OutputSchema JSON
}

// CodemodeTools returns detached program-callable declarations from the same
// phase-local captured registry/agent selection as nested dispatch.
func (r *TaskRuntime) CodemodeTools(ctx context.Context) ([]CodemodeToolDescription, error) {
	agent, err := r.Agent(ctx)
	if err != nil {
		return nil, err
	}
	object, err := dtoObject(agent.Configuration, r.harness.session.limits)
	if err != nil {
		return nil, err
	}
	var state agentState
	if err = fromObject(object, &state, r.harness.session.limits); err != nil {
		return nil, err
	}
	offers, _, _, _, err := r.phaseSelection().selectedTools(r.harness.selectionAgent(state), r.harness.session.limits, false, r.harness.scheduler.report)
	if err != nil {
		return nil, err
	}
	out := []CodemodeToolDescription{}
	ids := map[string]string{}
	for _, offer := range offers {
		if strings.HasPrefix(offer.Name, "pi.codemode.") || !toolCallerAllowed(offer, "tools") {
			continue
		}
		id := CodemodeIdentifier(offer.Name)
		if previous, ok := ids[id]; ok && previous != offer.Name {
			return nil, reject("codemode identifier collision")
		}
		ids[id] = offer.Name
		input, err := copyObject(offer.Schema, r.harness.session.limits)
		if err != nil {
			return nil, err
		}
		output, err := copyObjectNullable(offer.StructuredOutputSchema, r.harness.session.limits)
		if err != nil {
			return nil, err
		}
		if output == nil {
			output = JSON{"type": "string"}
		}
		out = append(out, CodemodeToolDescription{Name: offer.Name, Deferred: offer.Deferred, Identifier: id, Description: offer.Description, InputSchema: input, OutputSchema: output})
	}
	return out, nil
}
func CodemodeIdentifier(name string) string {
	if name == "" {
		return "_"
	}
	var b strings.Builder
	for i, unit := range utf16.Encode([]rune(name)) {
		r := rune(unit)
		valid := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' || r == '$' || i > 0 && r >= '0' && r <= '9'
		if valid {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
func CodemodeNamespaceOf(name string, namespaces []CodemodeNamespace) *CodemodeNamespace {
	var found *CodemodeNamespace
	for i := range namespaces {
		ns := namespaces[i]
		if strings.HasPrefix(name, ns.Prefix) && (found == nil || len(ns.Prefix) > len(found.Prefix)) {
			copy := ns
			found = &copy
		}
	}
	return found
}
func codemodeToolSection(tool CodemodeToolDescription) string {
	heading := "### `" + tool.Identifier + "`"
	if tool.Identifier != tool.Name {
		heading += " (`" + tool.Name + "`)"
	}
	return heading + "\n" + strings.TrimSpace(tool.Description) + "\n\n```typescript\ndeclare const tools: {\n  " + tool.Identifier + "(args: " + codemodeSchemaType(tool.InputSchema, 0) + "): Promise<" + codemodeSchemaType(tool.OutputSchema, 0) + ">;\n};\n```"
}

// CodemodeSection uses upstream namespace round-robin cheapest-first budget
// selection. UTF16 length matches TypeScript's character/token estimate.
func CodemodeSection(tools []CodemodeToolDescription, namespaces []CodemodeNamespace, budget int) (string, error) {
	if budget < 0 {
		return "", reject("negative codemode budget")
	}
	if len(tools) == 0 {
		return "", nil
	}
	type entry struct {
		name, section string
		cost          int
	}
	type group struct {
		namespace *CodemodeNamespace
		entries   []entry
		queue     []entry
	}
	groups := map[string]*group{"": {}}
	for _, tool := range tools {
		ns := CodemodeNamespaceOf(tool.Name, namespaces)
		key := ""
		if ns != nil {
			key = ns.Name
		}
		g := groups[key]
		if g == nil {
			g = &group{namespace: ns}
			groups[key] = g
		}
		section := codemodeToolSection(tool)
		g.entries = append(g.entries, entry{tool.Name, section, (len(utf16.Encode([]rune(section))) + 3) / 4})
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	active := []*group{}
	for _, key := range keys {
		g := groups[key]
		g.queue = append([]entry{}, g.entries...)
		sort.SliceStable(g.queue, func(i, j int) bool { return g.queue[i].cost < g.queue[j].cost })
		if len(g.queue) > 0 {
			active = append(active, g)
		}
	}
	shown := map[string]bool{}
	remaining := budget
	for len(active) > 0 {
		next := []*group{}
		for _, g := range active {
			item := g.queue[0]
			if item.cost > remaining {
				continue
			}
			remaining -= item.cost
			shown[item.name] = true
			g.queue = g.queue[1:]
			if len(g.queue) > 0 {
				next = append(next, g)
			}
		}
		active = next
	}
	parts := []string{"Tools scripts can call (`await tools.<name>(args)`):"}
	for _, key := range keys {
		g := groups[key]
		if len(g.entries) == 0 {
			continue
		}
		visible := 0
		for _, entry := range g.entries {
			if shown[entry.name] {
				visible++
			}
		}
		if g.namespace != nil {
			note := ""
			if visible == 0 {
				note = " (tools not listed)"
			} else if visible < len(g.entries) {
				note = " (some tools not listed)"
			}
			heading := "## " + g.namespace.Name + note
			if description := strings.TrimSpace(g.namespace.Description); description != "" {
				heading += "\n" + description
			}
			parts = append(parts, heading)
		} else if visible < len(g.entries) {
			parts = append(parts, "Some tools are not listed: find them with `searchTools()`.")
		}
		for _, entry := range g.entries {
			if shown[entry.name] {
				parts = append(parts, entry.section)
			}
		}
	}
	return strings.Join(parts, "\n\n"), nil
}
func SearchCodemodeTools(query string, tools []CodemodeToolDescription, namespaces []CodemodeNamespace, namespace string, limit int) []RankedDocument {
	docs := []SearchDocument{}
	for _, tool := range tools {
		if namespace != "" {
			ns := CodemodeNamespaceOf(tool.Name, namespaces)
			if ns == nil || ns.Name != namespace {
				continue
			}
		}
		docs = append(docs, SearchDocument{tool.Name, fmt.Sprintf("%s %s %s", tool.Name, tool.Description, schemaSearchText(map[string]any(tool.InputSchema)))})
	}
	return RankDocuments(query, docs, limit)
}
