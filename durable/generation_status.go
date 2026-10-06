package durable

import "fmt"

// Project adopted checkpoint state into the harness presentation document in the
// same commit. No provider event becomes visible before checkpoint adoption.
func (t *Tx) syncGenerationStatuses() error {
	conversations := map[ID]bool{}
	for _, write := range t.writes {
		if write.Task != nil && (write.Task.Kind == "pi.generation" || write.Task.Kind == "pi.tool") {
			conversations[write.Task.Conversation] = true
		}
	}
	if len(conversations) == 0 {
		return nil
	}
	state, err := t.current()
	if err != nil {
		return err
	}
	for _, conversation := range ids(conversations) {
		var live Document
		for _, document := range state.Documents {
			if document.Kind == "pi.live" && document.Scope == "conversation" && document.Owner == conversation && !document.Retired {
				live = document
				break
			}
		}
		if live.ID == 0 {
			continue
		}
		value, err := copyObject(live.Value, t.limits)
		if err != nil {
			return err
		}
		run, _ := value["run"].(map[string]any)
		number, ok := exactNumber(run["task"])
		if !ok {
			delete(value, "generation")
			delete(value, "tools")
		} else {
			task, exists := state.Tasks[ID(number.Num().Uint64())]
			if exists && task.Kind == "pi.generation" && !terminalStatus(task.Status) && !taskHasDecidedOutcome(task) {
				var cp generationCheckpoint
				if err := fromObject(task.Checkpoint, &cp, t.limits); err != nil {
					return err
				}
				if cp.Phase == "tools" {
					delete(value, "generation")
					slots, err := t.generationToolSlots(state, task, cp)
					if err != nil {
						return err
					}
					value["tools"] = slots
				} else {
					delete(value, "tools")
					if cp.Phase == "intent" || cp.Phase == "retry" || cp.Phase == "poll" || cp.Partial != nil {
						generation := JSON{"attempt": cp.Attempt}
						if cp.Partial != nil {
							message, err := dtoObject(*cp.Partial, t.limits)
							if err != nil {
								return err
							}
							generation["message"] = message
						}
						if cp.Phase == "retry" {
							generation["retry"] = JSON{"at": cp.RetryUntil, "error": cp.RetryMessage}
						}
						if cp.Phase == "poll" {
							generation["deferred"] = JSON{"pollAt": cp.PollAt}
						}
						value["generation"] = generation
					} else {
						delete(value, "generation")
					}
				}
			} else {
				delete(value, "generation")
				delete(value, "tools")
			}
		}
		if equalJSONValue(value, live.Value) {
			continue
		}
		live.Value, live.DeltasSinceBase = value, 0
		if err := t.stage(Write{Op: "put-document", Document: &live}); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tx) generationToolSlots(state Snapshot, parent Task, cp generationCheckpoint) ([]any, error) {
	assistant, ok := state.Entries[cp.AssistantEntry]
	if !ok {
		return nil, reject("tool slots assistant missing")
	}
	var message MessageReceipt
	if err := fromObject(assistant.Value, &message, t.limits); err != nil {
		return nil, err
	}
	tasks := map[string]Task{}
	for _, id := range cp.Children {
		task, ok := state.Tasks[id]
		if !ok {
			continue
		}
		var tool toolCheckpoint
		if err := fromObject(task.Checkpoint, &tool, t.limits); err != nil {
			return nil, err
		}
		tasks[tool.CallID] = task
	}
	entries := map[string]ID{}
	for _, entry := range state.Entries {
		if entry.Conversation == parent.Conversation && entry.ByTask == parent.ID && entry.Value["role"] == "toolResult" {
			if call, ok := entry.Value["toolCallId"].(string); ok {
				entries[call] = entry.ID
			}
		}
	}
	slots := []any{}
	for _, call := range message.Content {
		if call.Type != "toolCall" {
			continue
		}
		slot := JSON{"callId": call.ID, "name": call.Name, "status": "pending"}
		if task, ok := tasks[call.ID]; ok {
			slot["taskId"] = task.ID
			var tool toolCheckpoint
			if err := fromObject(task.Checkpoint, &tool, t.limits); err != nil {
				return nil, err
			}
			if tool.Started {
				slot["status"] = "running"
			}
			if terminalStatus(task.Status) || taskHasDecidedOutcome(task) {
				slot["status"] = "done"
				if task.Execution != nil && task.Execution.Builtin.Hold != nil && task.Execution.Builtin.Hold.Entry != 0 {
					slot["entry"] = task.Execution.Builtin.Hold.Entry
				}
			} else {
				if tool.Output != "" {
					slot["output"] = tool.Output
				}
				if tool.HasDetails {
					slot["details"] = tool.Details
				}
				if len(tool.Diagnostics) > 0 {
					diagnostics, err := dtoObject(JSON{"values": tool.Diagnostics}, t.limits)
					if err != nil {
						return nil, err
					}
					slot["diagnostics"] = diagnostics["values"]
				}
				if tool.DroppedBytes > 0 {
					slot["droppedBytes"] = tool.DroppedBytes
				}
				if tool.DroppedLines > 0 {
					slot["droppedLines"] = tool.DroppedLines
				}
			}
		} else if entry, ok := entries[call.ID]; ok {
			slot["status"], slot["entry"] = "done", entry
		}
		slots = append(slots, slot)
	}
	if len(slots) == 0 {
		return nil, reject(fmt.Sprintf("tool round %d has no calls", parent.ID))
	}
	return slots, nil
}
