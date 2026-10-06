package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// AgentPatch changes only supplied fields. Clear removes optional stored choices;
// nil fields are unchanged. Configure remains the native full replacement API.
// Clearing model leaves the conversation unconfigured until a later patch.
type AgentPatch struct {
	Model           *ModelRef
	ThinkingLevel   *goai.ModelThinkingLevel
	Name            *string
	Cwd             *string
	SystemPrompt    *string
	Instructions    *string
	Settings        *RequestSettings
	Extensions      *[]string
	Tools           *[]string
	ExtensionFilter *ExtensionFilter
	ToolsRemoved    *[]string
	Clear           []string // model, name, cwd, thinkingLevel, systemPrompt, instructions, settings, extensions, tools
}

func (c *ConversationHandle) ConfigurePatch(ctx context.Context, patch AgentPatch) error {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return ErrClosed
	}
	_, err := h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if _, ok := tx.state.Conversations[c.id]; !ok {
			return reject("unknown conversation")
		}
		doc, found := agentDocument(tx.state, c.id)
		var state agentState
		if found {
			if err := fromObject(doc.Value, &state, tx.limits); err != nil {
				return err
			}
		}
		supplied := map[string]bool{"model": patch.Model != nil, "thinkingLevel": patch.ThinkingLevel != nil, "name": patch.Name != nil, "cwd": patch.Cwd != nil, "systemPrompt": patch.SystemPrompt != nil, "instructions": patch.Instructions != nil, "settings": patch.Settings != nil, "extensions": patch.Extensions != nil || patch.ExtensionFilter != nil, "tools": patch.Tools != nil || patch.ToolsRemoved != nil}
		seen := map[string]bool{}
		for _, field := range patch.Clear {
			if seen[field] || supplied[field] {
				return reject("conflicting agent patch")
			}
			seen[field] = true
			switch field {
			case "model":
				state.Model = ModelRef{}
			case "thinkingLevel":
				state.ThinkingLevel = ""
			case "name":
				state.Name = ""
			case "cwd":
				state.Cwd = ""
			case "systemPrompt":
				state.SystemPrompt = ""
			case "instructions":
				state.Instructions = nil
			case "settings":
				state.Settings = RequestSettings{}
			case "extensions":
				state.Extensions = nil
				state.ExtensionFilter = nil
			case "tools":
				state.Tools = nil
				state.ToolsRemoved = nil
			default:
				return reject("invalid agent clear field")
			}
		}
		if patch.Model != nil {
			state.Model = *patch.Model
		}
		if patch.ThinkingLevel != nil {
			state.ThinkingLevel = *patch.ThinkingLevel
		}
		if patch.Name != nil {
			state.Name = *patch.Name
		}
		if patch.Cwd != nil {
			state.Cwd = *patch.Cwd
		}
		if patch.SystemPrompt != nil {
			state.SystemPrompt = *patch.SystemPrompt
		}
		if patch.Instructions != nil {
			state.Instructions = patch.Instructions
		}
		if patch.Settings != nil {
			state.Settings = *patch.Settings
		}
		if patch.Extensions != nil && patch.ExtensionFilter != nil || patch.Tools != nil && patch.ToolsRemoved != nil {
			return reject("conflicting agent selection")
		}
		if patch.Extensions != nil {
			state.Extensions = patch.Extensions
			state.ExtensionFilter = nil
		}
		if patch.ExtensionFilter != nil {
			state.ExtensionFilter = patch.ExtensionFilter
			state.Extensions = nil
		}
		if patch.Tools != nil {
			state.Tools = patch.Tools
			state.ToolsRemoved = nil
		}
		if patch.ToolsRemoved != nil {
			state.ToolsRemoved = patch.ToolsRemoved
			state.Tools = nil
		}
		validated, err := h.agent(AgentChange{Name: state.Name, Cwd: state.Cwd, Model: state.Model, ThinkingLevel: state.ThinkingLevel, SystemPrompt: state.SystemPrompt, Instructions: state.Instructions, Settings: state.Settings, Extensions: state.Extensions, Tools: state.Tools, ExtensionFilter: state.ExtensionFilter, ToolsRemoved: state.ToolsRemoved})
		if err != nil {
			return err
		}
		value, err := dtoObject(validated, tx.limits)
		if err != nil {
			return err
		}
		if !found {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: c.id, Kind: "pi.agent", Version: 1, History: "rewindable", Fork: "asOf", Value: value})
			if err != nil {
				return err
			}
			return initializeBuiltins(tx, c.id)
		}
		agent, err := builtin(tx, c.id, "pi.agent")
		if err != nil {
			return err
		}
		return agent.Set(value)
	})
	return err
}

func agentSelectionNames(input []string) ([]string, error) {
	if len(input) > DefaultLimits().MaxPage {
		return nil, reject("agent selection limit")
	}
	result := []string{}
	seen := map[string]bool{}
	for _, name := range input {
		if !validKind(name) {
			return nil, reject("invalid agent selection")
		}
		if !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result, nil
}

// selectedExtensionsLocked follows explicit selection order; missing names do
// not resolve. Native default selection retains registry installation order.
func (r *Registry) selectedExtensionsLocked(agent agentState) []installedExtension {
	names := agent.Extensions
	if names == nil && agent.ExtensionFilter == nil && agent.defaultExtensions == nil {
		return r.extensions
	}
	if names == nil {
		selected := []string{}
		removed := map[string]bool{}
		if agent.ExtensionFilter != nil {
			for _, name := range agent.ExtensionFilter.Remove {
				removed[name] = true
			}
		}
		if agent.defaultExtensions != nil {
			selected = append(selected, (*agent.defaultExtensions)...)
		} else {
			for _, extension := range r.extensions {
				selected = append(selected, extension.name)
			}
		}
		if agent.ExtensionFilter != nil {
			selected = append(selected, agent.ExtensionFilter.Add...)
		}
		kept := selected[:0]
		for _, name := range selected {
			if !removed[name] {
				kept = append(kept, name)
			}
		}
		names = &kept
	}
	ordered := make([]installedExtension, 0, len(*names))
	seen := map[string]bool{}
	for _, name := range *names {
		if seen[name] {
			continue
		}
		seen[name] = true
		for _, extension := range r.extensions {
			if extension.name == name {
				ordered = append(ordered, extension)
				break
			}
		}
	}
	return ordered
}
