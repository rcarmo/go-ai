package durable

// A tool slot is authority only for the matching live generation, invocation
// and call. Old checkpoints remain a fallback when no such slot is present.
func toolPresentationSlot(value JSON, task Task, callID string) JSON {
	run := generationObject(value["run"])
	owner, ok := exactNumber(run["task"])
	if !ok || !owner.IsInt() || !owner.Num().IsUint64() || owner.Num().Uint64() != uint64(task.Owner) {
		return nil
	}
	slots, _ := value["tools"].([]any)
	for _, raw := range slots {
		slot := generationObject(raw)
		id, ok := exactNumber(slot["taskId"])
		if ok && id.IsInt() && id.Num().IsUint64() && id.Num().Uint64() == uint64(task.ID) && slot["callId"] == callID {
			return slot
		}
	}
	return nil
}

func toolPresentation(state Snapshot, task Task, cp toolCheckpoint, limits Limits) (toolCheckpoint, error) {
	for _, doc := range state.Documents {
		if doc.Kind != "pi.live" || doc.Scope != "conversation" || doc.Owner != task.Conversation || doc.Retired {
			continue
		}
		slot := toolPresentationSlot(doc.Value, task, cp.CallID)
		if slot == nil || slot["status"] == "done" {
			continue
		}
		value := JSON{}
		for _, key := range []string{"output", "details", "diagnostics", "droppedBytes", "droppedLines"} {
			if v, ok := slot[key]; ok {
				value[key] = v
			}
		}
		if _, ok := slot["details"]; ok {
			value["hasDetails"] = true
		}
		var progress toolCheckpoint
		if err := fromObject(value, &progress, limits); err != nil {
			return cp, err
		}
		copyToolProgress(&cp, progress)
		return cp, nil
	}
	return cp, nil
}

func setToolPresentation(slot JSON, progress JSON, hasDetails bool) {
	for _, key := range []string{"output", "diagnostics", "droppedBytes", "droppedLines"} {
		if value, ok := progress[key]; ok {
			slot[key] = value
		} else {
			delete(slot, key)
		}
	}
	if hasDetails {
		slot["details"] = progress["details"]
	} else {
		delete(slot, "details")
	}
}

func clearToolPresentation(tx *Tx, task Task, callID string) error {
	live, err := builtin(tx, task.Conversation, "pi.live")
	if err != nil {
		return err
	}
	return live.Update(func(value JSON) error {
		if slot := toolPresentationSlot(value, task, callID); slot != nil {
			for _, key := range []string{"output", "details", "diagnostics", "droppedBytes", "droppedLines"} {
				delete(slot, key)
			}
		}
		return nil
	})
}
