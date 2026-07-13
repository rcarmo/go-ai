package goai

import "strings"

// DeferredToolPlan classifies the active tool set for providers that support
// upstream pi-ai deferred tool loading. Immediate tools are sent in the normal
// request tool list; Deferred tools are introduced at tool-result markers via
// provider-specific references/search output.
type DeferredToolPlan struct {
	Immediate []Tool
	Deferred  []Tool
	ByName    map[string]Tool
}

func (p DeferredToolPlan) HasDeferred() bool { return len(p.Deferred) > 0 }

// PlanDeferredTools applies upstream deferred-tools semantics:
//   - markers come from ToolResultMessage.addedToolNames;
//   - a marker only counts if the tool is still present in Context.tools;
//   - tools already used before their marker remain immediate;
//   - unsupported providers, missing markers, or all-tools-marked fall back to
//     the normal tool list;
//   - OAuth canonicalization deduplicates names such as read/Read.
func PlanDeferredTools(ctx *Context, supportsDeferred bool, canonicalizeOAuthNames bool) DeferredToolPlan {
	plan := DeferredToolPlan{ByName: map[string]Tool{}}
	if ctx == nil || len(ctx.Tools) == 0 {
		return plan
	}
	active := canonicalTools(ctx.Tools, canonicalizeOAuthNames)
	for _, t := range active {
		plan.ByName[t.Name] = t
	}
	if !supportsDeferred {
		plan.Immediate = append(plan.Immediate, active...)
		return plan
	}

	used := map[string]bool{}
	deferred := map[string]bool{}
	for _, msg := range ctx.Messages {
		switch msg.Role {
		case RoleAssistant:
			for _, c := range msg.Content {
				if c.Type == "toolCall" {
					used[canonicalToolName(c.Name, canonicalizeOAuthNames)] = true
				}
			}
		case RoleToolResult:
			for _, name := range msg.AddedToolNames {
				canon := canonicalToolName(name, canonicalizeOAuthNames)
				if _, ok := plan.ByName[canon]; ok && !used[canon] {
					deferred[canon] = true
				}
			}
		}
	}
	if len(deferred) == 0 || len(deferred) >= len(active) {
		plan.Immediate = append(plan.Immediate, active...)
		plan.Deferred = nil
		return plan
	}
	for _, t := range active {
		if deferred[t.Name] {
			plan.Deferred = append(plan.Deferred, t)
		} else {
			plan.Immediate = append(plan.Immediate, t)
		}
	}
	return plan
}

func canonicalTools(tools []Tool, canonicalize bool) []Tool {
	out := make([]Tool, 0, len(tools))
	seen := map[string]int{}
	for _, t := range tools {
		t.Name = canonicalToolName(t.Name, canonicalize)
		if idx, ok := seen[t.Name]; ok {
			out[idx] = t
			continue
		}
		seen[t.Name] = len(out)
		out = append(out, t)
	}
	return out
}

func canonicalToolName(name string, canonicalize bool) string {
	if !canonicalize {
		return name
	}
	key := strings.ToLower(strings.ReplaceAll(name, "_", ""))
	switch key {
	case "bash", "glob", "grep", "ls", "read", "edit", "write", "notebookedit", "webfetch", "websearch", "todowrite", "task", "taskoutput", "skill", "killshell":
		return strings.ToUpper(key[:1]) + key[1:]
	default:
		return name
	}
}

// DeferredToolsForMarker returns deferred tools named by a single tool-result
// marker, after active-tool and canonical-name filtering.
func DeferredToolsForMarker(plan DeferredToolPlan, names []string, canonicalizeOAuthNames bool) []Tool {
	if len(plan.Deferred) == 0 || len(names) == 0 {
		return nil
	}
	deferred := map[string]Tool{}
	for _, t := range plan.Deferred {
		deferred[t.Name] = t
	}
	var out []Tool
	seen := map[string]bool{}
	for _, name := range names {
		canon := canonicalToolName(name, canonicalizeOAuthNames)
		if t, ok := deferred[canon]; ok && !seen[canon] {
			out = append(out, t)
			seen[canon] = true
		}
	}
	return out
}
