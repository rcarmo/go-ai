package durable

func nestedLifecycleEvents(state Snapshot, old, task Task, seq uint64, l Limits) ([]AgentEvent, error) {
	record, err := CanonicalTask(task, l)
	if err != nil {
		return nil, err
	}
	input, err := nestedInput(record, l)
	if err != nil {
		return nil, err
	}
	callID := input.Key
	if input.ParentCallID != "" {
		callID = input.ParentCallID + "/" + input.Key
	}
	base := AgentEvent{Seq: seq, Task: task.ID, ParentTask: task.Owner, ToolCallID: callID, ParentToolCallID: input.ParentCallID, ToolName: input.Name}
	events := []AgentEvent{}
	currentCheckpoint := task.Checkpoint
	if task.Execution != nil && task.Execution.Native != nil {
		currentCheckpoint = task.Execution.Native.State.Checkpoint
	}
	oldCheckpoint := old.Checkpoint
	if old.Execution != nil && old.Execution.Native != nil {
		oldCheckpoint = old.Execution.Native.State.Checkpoint
	}
	started, _ := currentCheckpoint["started"].(bool)
	wasStarted, _ := oldCheckpoint["started"].(bool)
	if started && !wasStarted {
		event := base
		event.Type = "tool_execution_start"
		arguments := input.Arguments
		if rewritten, ok := currentCheckpoint["arguments"].(map[string]any); ok {
			arguments = JSON(rewritten)
		}
		event.Args, err = copyObject(arguments, l)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	before, err := nestedEventProgress(oldCheckpoint, l)
	if err != nil {
		return nil, err
	}
	after, err := nestedEventProgress(currentCheckpoint, l)
	if err != nil {
		return nil, err
	}
	if !equalJSONValue(before, after) {
		event := base
		event.Type = "tool_execution_update"
		event.Output = after.Output
		event.OutputChange = outputChange(before.Output, after.Output)
		event.DroppedBytes, event.DroppedLines = after.DroppedBytes, after.DroppedLines
		if before.HasDetails != after.HasDetails || !equalJSONValue(before.Details, after.Details) {
			event.HasDetails = true
			event.Details, err = ownJSONValue(after.Details, l)
			if err != nil {
				return nil, err
			}
		}
		if !equalJSONValue(before.Diagnostics, after.Diagnostics) {
			from := 0
			if len(before.Diagnostics) <= len(after.Diagnostics) && equalJSONValue(before.Diagnostics, after.Diagnostics[:len(before.Diagnostics)]) {
				from = len(before.Diagnostics)
			}
			event.Diagnostics = append([]ToolDiagnostic{}, after.Diagnostics[from:]...)
		}
		events = append(events, event)
	}
	if taskHasDecidedOutcome(task) && !taskHasDecidedOutcome(old) {
		event := base
		event.Type = "tool_execution_end"
		for _, doc := range state.Documents {
			if doc.Kind != nestedCallDocument || doc.Owner != task.Owner || doc.Key != input.Key {
				continue
			}
			var call nestedCallState
			if err := fromObject(doc.Value, &call, l); err != nil {
				return nil, err
			}
			if call.Task != task.ID || call.Result == nil {
				return nil, reject("nested event result missing")
			}
			receipt := call.Result
			result := ToolResult{Content: receipt.Content, Blocks: receipt.Blocks, Details: receipt.Details, HasDetails: receipt.HasDetails, Diagnostics: receipt.Diagnostics, IsError: receipt.IsError, StructuredOutput: receipt.StructuredOutput, HasStructuredOutput: receipt.HasStructuredOutput}
			owned, err := detachToolResult(result, l)
			if err != nil {
				return nil, err
			}
			event.NestedResult = &owned
			break
		}

		events = append(events, event)
	}
	return events, nil
}
func nestedEventProgress(checkpoint JSON, l Limits) (toolCheckpoint, error) {
	var progress toolCheckpoint
	if value, ok := checkpoint["progress"].(map[string]any); ok {
		if err := fromObject(JSON(value), &progress, l); err != nil {
			return progress, err
		}
	}
	return progress, nil
}
