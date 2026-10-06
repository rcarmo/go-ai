package durable

import "unicode/utf8"

// ToolControl is the pinned post-tools request. Completed tool outcomes alone
// participate; every call must request termination, and last handoff wins.
type ToolControl struct {
	AddTools  []string `json:"addTools,omitempty"`
	Terminate bool     `json:"terminate,omitempty"`
	Handoff   *string  `json:"handoff,omitempty"`
}

func validateToolControl(control *ToolControl, l Limits) error {
	if control == nil {
		return nil
	}
	if len(control.AddTools) > l.MaxPage {
		return reject("tool control names limit")
	}
	for _, name := range control.AddTools {
		if !validKind(name) {
			return reject("invalid tool control name")
		}
	}
	if control.Handoff != nil && (!utf8.ValidString(*control.Handoff) || len(*control.Handoff) > l.MaxStringBytes) {
		return reject("invalid tool handoff")
	}
	return nil
}
func (h *Harness) applyToolControls(tx *Tx, current Task, cp *generationCheckpoint, children []Task) (bool, error) {
	terminate := len(children) > 0 && len(cp.UnstartedCalls) == 0
	var handoff *string
	var added []string
	for _, child := range children {
		var tool toolCheckpoint
		if err := fromObject(child.Checkpoint, &tool, tx.limits); err != nil {
			return false, err
		}
		control := tool.Control
		if tool.ErrorCode != "" {
			control = nil
		}
		if control == nil {
			terminate = false
			continue
		}
		if !control.Terminate {
			terminate = false
		}
		added = append(added, control.AddTools...)
		if control.Handoff != nil {
			value := *control.Handoff
			handoff = &value
		}
	}
	if len(added) > 0 {
		agent, err := builtin(tx, current.Conversation, "pi.agent")
		if err != nil {
			return false, err
		}
		value, err := agent.Get()
		if err != nil {
			return false, err
		}
		var state agentState
		if err := fromObject(value, &state, tx.limits); err != nil {
			return false, err
		}
		// Nil already selects all, matching upstream undefined selection.
		if state.Tools != nil {
			selected := append([]string{}, (*state.Tools)...)
			seen := map[string]bool{}
			for _, name := range selected {
				seen[name] = true
			}
			for _, name := range added {
				if !seen[name] {
					selected = append(selected, name)
					seen[name] = true
				}
			}
			if len(selected) > tx.limits.MaxPage {
				return false, reject("agent tool selection limit")
			}
			state.Tools = &selected
			owned, err := dtoObject(state, tx.limits)
			if err != nil {
				return false, err
			}
			if err := agent.Set(owned); err != nil {
				return false, err
			}
		} else if state.ToolsRemoved != nil {
			allowed := map[string]bool{}
			for _, name := range added {
				allowed[name] = true
			}
			kept := []string{}
			for _, name := range *state.ToolsRemoved {
				if !allowed[name] {
					kept = append(kept, name)
				}
			}
			if len(kept) != len(*state.ToolsRemoved) {
				state.ToolsRemoved = &kept
				owned, err := dtoObject(state, tx.limits)
				if err != nil {
					return false, err
				}
				if err := agent.Set(owned); err != nil {
					return false, err
				}
			}
		}
	}
	if !terminate && handoff == nil {
		return false, nil
	}
	entry, ok := tx.state.Entries[cp.AssistantEntry]
	if !ok {
		return false, reject("tool round assistant unavailable")
	}
	var receipt MessageReceipt
	if err := fromObject(entry.Value, &receipt, tx.limits); err != nil {
		return false, err
	}
	if handoff != nil {
		if err := appendReset(tx, current.Conversation, *handoff); err != nil {
			return false, err
		}
	}
	cp.Phase = "terminal"
	cp.Children = nil
	owned, err := dtoObject(cp, tx.limits)
	if err != nil {
		return false, err
	}
	current.Checkpoint = owned
	current.Status = "done"
	hold := &BuiltinTaskHold{Stage: "final", Action: "generation-receipt", Outcome: TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": entry.ID}}}, FinalStatus: "done", Entry: entry.ID, Submission: cp.Submission, Conversation: current.Conversation, Owner: current.Owner}
	metadata := &BuiltinTaskExecution{}
	if current.Execution != nil {
		*metadata = *current.Execution.Builtin
	}
	metadata.Memos = nil
	metadata.Hold = hold
	current.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
	if len(h.scheduler.ownedLive(tx.state, current.ID)) > 0 {
		hold.Stage, current.Status = "held", "completing"
	}
	value, err := dtoObject(receipt, tx.limits)
	if err != nil {
		return false, err
	}
	if err := h.cleanupGeneration(tx, current, hold, value); err != nil {
		return false, err
	}
	if err := tx.stage(Write{Op: "put-task", Task: &current}); err != nil {
		return false, err
	}
	return true, nil
}
