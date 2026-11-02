package durable

func toolProgressEvent(before, after toolCheckpoint, base AgentEvent, limits Limits) (*AgentEvent, error) {
	detailsChanged := before.HasDetails != after.HasDetails || !equalJSONValue(before.Details, after.Details)
	diagnosticsChanged := !equalJSONValue(before.Diagnostics, after.Diagnostics)
	if after.Output == before.Output && !detailsChanged && !diagnosticsChanged && after.DroppedBytes == before.DroppedBytes && after.DroppedLines == before.DroppedLines {
		return nil, nil
	}
	event := base
	event.Type = "tool_execution_update"
	event.DroppedBytes, event.DroppedLines = after.DroppedBytes, after.DroppedLines
	if diagnosticsChanged {
		from := 0
		if len(before.Diagnostics) <= len(after.Diagnostics) && equalJSONValue(before.Diagnostics, after.Diagnostics[:len(before.Diagnostics)]) {
			from = len(before.Diagnostics)
		}
		event.Diagnostics = append([]ToolDiagnostic{}, after.Diagnostics[from:]...)
	}
	if after.Output != before.Output {
		event.Output = after.Output
		event.OutputChange = outputChange(before.Output, after.Output)
	}
	if detailsChanged {
		event.HasDetails = true
		var err error
		event.Details, err = ownJSONValue(after.Details, limits)
		if err != nil {
			return nil, err
		}
	}
	return &event, nil
}

// Document-only memory progress has no task-table write. Stored flushes compare
// against the same logical memory value, so they cannot replay output events.
func toolDocumentEvents(before, next Snapshot, frame PublicationFrame, conversation ID, limits Limits) ([]AgentEvent, error) {
	var events []AgentEvent
	for _, change := range frame.Publication.Documents {
		if change.Record.Kind != "pi.live" || change.Record.Owner != conversation {
			continue
		}
		doc := next.Documents[change.Record.ID]
		slots, _ := doc.Value["tools"].([]any)
		for _, raw := range slots {
			slot := generationObject(raw)
			number, ok := exactNumber(slot["taskId"])
			if !ok || !number.IsInt() || !number.Num().IsUint64() {
				continue
			}
			task, ok := next.Tasks[ID(number.Num().Uint64())]
			if !ok || task.Kind != "pi.tool" || task.Conversation != conversation {
				continue
			}
			changed := false
			for _, write := range frame.Publication.Tables {
				if write.Task != nil && write.Task.ID == task.ID {
					changed = true
					break
				}
			}
			if changed {
				continue
			} // table path also handles starts, clears and endings
			var cp toolCheckpoint
			if err := fromObject(task.Checkpoint, &cp, limits); err != nil {
				return nil, err
			}
			a, err := toolPresentation(before, task, cp, limits)
			if err != nil {
				return nil, err
			}
			b, err := toolPresentation(next, task, cp, limits)
			if err != nil {
				return nil, err
			}
			event, err := toolProgressEvent(a, b, AgentEvent{Seq: next.Seq, Task: task.ID, ToolCallID: cp.CallID, ToolName: cp.Offer.Name}, limits)
			if err != nil {
				return nil, err
			}
			if event != nil {
				events = append(events, *event)
			}
		}
	}
	return events, nil
}
