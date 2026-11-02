package durable

import "context"

func generationObject(value any) JSON {
	switch object := value.(type) {
	case JSON:
		return object
	case map[string]any:
		return JSON(object)
	}
	return nil
}

// Legacy checkpoints remain readable. New partials live only in the logical
// presentation document, so reopening after failure reads only stored output.
func generationPartial(state Snapshot, task Task, cp generationCheckpoint, limits Limits) (*messageReceipt, error) {
	if cp.Partial != nil {
		return cp.Partial, nil
	}
	for _, live := range state.Documents {
		if live.Kind != "pi.live" || live.Owner != task.Conversation || live.Retired {
			continue
		}
		run := generationObject(live.Value["run"])
		id, ok := exactNumber(run["task"])
		if !ok || !id.IsInt() || id.Num().Uint64() != uint64(task.ID) {
			continue
		}
		generation := generationObject(live.Value["generation"])
		value := generationObject(generation["message"])
		if value == nil {
			return nil, nil
		}
		var partial messageReceipt
		if err := fromObject(value, &partial, limits); err != nil {
			return nil, err
		}
		return &partial, nil
	}
	return nil, nil
}

func clearGenerationPresentation(tx *Tx, conversation ID) error {
	live, err := builtin(tx, conversation, "pi.live")
	if err != nil {
		return err
	}
	return live.Update(func(value JSON) error { delete(value, "generation"); return nil })
}

func (h *Harness) commitGenerationPresentation(runtime *TaskRuntime, id ID, partial *messageReceipt) error {
	var message JSON
	if partial != nil {
		var err error
		message, err = dtoObject(*partial, h.session.limits)
		if err != nil {
			return err
		}
	}
	change := func(tx *Tx) error {
		task, ok := tx.state.Tasks[id]
		if !ok || terminalStatus(task.Status) || taskHasDecidedOutcome(task) {
			return ErrSealed
		}
		var cp generationCheckpoint
		if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		if cp.Abort {
			return reject("generation aborted")
		}
		live, err := builtin(tx, task.Conversation, "pi.live")
		if err != nil {
			return err
		}
		return live.Update(func(value JSON) error {
			generation := JSON{"attempt": cp.Attempt}
			if partial != nil {
				generation["message"] = message
			}
			value["generation"] = generation
			return nil
		})
	}
	if runtime != nil {
		return runtime.commitProgress(runtime.context, func(tx *Tx, _ TaskRecord) (*TaskState, error) { return nil, change(tx) })
	}
	// Kept immediate for legacy/internal callers without a bound runtime permit.
	_, err := h.session.invocationCommit(context.Background(), id, change)
	return err
}
