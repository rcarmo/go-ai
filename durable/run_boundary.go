package durable

// startQueuedRunBoundary places the final inbox boundary and admits its next
// run in the same transaction. InputEntry remains each input's own entry;
// first is the first entry actually placed by this boundary, including writes.
func (h *Harness) startQueuedRunBoundary(tx *Tx, conversation, ending ID, keepFailure bool, decisions ...Task) error {
	live, err := builtin(tx, conversation, "pi.live")
	if err != nil {
		return err
	}
	value, err := live.Get()
	if err != nil {
		return err
	}
	if len(generationObject(value["run"])) != 0 {
		return nil
	}
	first, err := h.placeFinalBoundaryWrites(tx, conversation)
	if err != nil {
		return err
	}
	state, err := tx.current()
	if err != nil {
		return err
	}
	selection := boundarySelection(state, decisions)
	for _, id := range ids(state.Tasks) {
		task := state.Tasks[id]
		if id == ending || task.Kind != "pi.generation" || task.Conversation != conversation || terminalStatus(task.Status) || taskHasDecidedOutcome(task) || taskAborted(task) || taskBelowCancelled(selection, task) || taskSelectedFailFastCancellation(selection, id) || h.scheduler.invocations[id] != nil || len(h.scheduler.ownedLive(state, id)) != 0 {
			continue
		}
		var cp generationCheckpoint
		if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		sub, ok := state.Submissions[cp.Submission]
		if !ok || cp.Phase != "queued" || terminalStatus(sub.Status) || cp.InputEntry != 0 {
			continue
		}
		entryID, err := tx.MintID()
		if err != nil {
			return err
		}
		input, err := inputReceipt(cp.Input, cp.InputBlocks, tx.limits)
		if err != nil {
			return err
		}
		wire, err := dtoObject(input, tx.limits)
		if err != nil {
			return err
		}
		if err := tx.AppendEntry(inputMetadataEntry(cp.InputMetadata, entryID, conversation, wire, input, tx.byTask)); err != nil {
			return err
		}
		cp.InputEntry = entryID
		if first == 0 {
			first = entryID
		}
		if err := markSubmissionEntry(tx, cp.Submission, entryID); err != nil {
			return err
		}
		if err := h.applyQueuedInputs(tx, task, &cp, true, decisions...); err != nil {
			return err
		}
		task.Checkpoint, err = dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
			return err
		}
		if err := removeRunInputs(tx, conversation, cp); err != nil {
			return err
		}
		inputs := []any{cp.Submission}
		for _, input := range cp.Steered {
			inputs = append(inputs, input)
		}
		return live.Update(func(value JSON) error {
			if len(generationObject(value["run"])) != 0 {
				return reject("generation run ownership changed")
			}
			value["run"] = JSON{"task": task.ID, "inputs": inputs, "first": first}
			if !keepFailure {
				delete(value, "runFailure")
			}
			return nil
		})
	}
	return nil
}

func removeRunInputs(tx *Tx, conversation ID, cp generationCheckpoint) error {
	inbox, err := builtin(tx, conversation, "pi.inbox")
	if err != nil {
		return err
	}
	return inbox.Update(func(value JSON) error {
		items, ok := value["items"].([]any)
		if !ok {
			return reject("inbox shape")
		}
		keep := []any{}
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				return reject("inbox item")
			}
			if item["mode"] == "reset" || !generationIncludesSubmission(cp, submissionEntryID(item["id"])) {
				keep = append(keep, raw)
			}
		}
		value["items"] = keep
		return nil
	})
}

// Reset entries and passive writes share one ordered boundary lane. A stale
// write appends nothing and cannot supply first; no historical scan is used.
func (h *Harness) placeFinalBoundaryWrites(tx *Tx, conversation ID) (ID, error) {
	inbox, err := builtin(tx, conversation, "pi.inbox")
	if err != nil {
		return 0, err
	}
	value, err := inbox.Get()
	if err != nil {
		return 0, err
	}
	items, ok := value["items"].([]any)
	if !ok {
		return 0, reject("inbox shape")
	}
	var first ID
	keep := []any{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return 0, reject("inbox item")
		}
		if item["mode"] == "reset" {
			text, _ := item["content"].(string)
			id, err := appendResetEntry(tx, conversation, text)
			if err != nil {
				return 0, err
			}
			if first == 0 {
				first = id
			}
			continue
		}
		if item["mode"] != "write" {
			keep = append(keep, raw)
			continue
		}
		state, err := tx.current()
		if err != nil {
			return 0, err
		}
		sub, ok := state.Submissions[submissionEntryID(item["id"])]
		if !ok || sub.Type != "write" {
			return 0, reject("write submission missing")
		}
		if terminalStatus(sub.Status) {
			continue
		}
		sub.Value, err = copyObject(sub.Value, tx.limits)
		if err != nil {
			return 0, err
		}
		stale, err := passiveHeadStale(state, conversation, sub.Value, tx.limits)
		if err != nil {
			return 0, err
		}
		if stale {
			sub.Status, sub.Value["errorCode"] = "failed", "stale"
		} else {
			if err := appendPassiveWrite(tx, conversation, sub.Value); err != nil {
				return 0, err
			}
			sub.Status = "done"
			if first == 0 {
				first, _ = sub.Value["placedEntry"].(ID)
			}
		}
		if err := tx.PutSubmission(sub); err != nil {
			return 0, err
		}
	}
	if err := inbox.Update(func(value JSON) error { value["items"] = keep; return nil }); err != nil {
		return 0, err
	}
	return first, nil
}

// A boundary's cancellation predicates must see the locally decided outcome,
// even before the caller's single authoritative task write. This private task
// table copy is selection-only: it never changes the candidate/adopted tables,
// write budget, task timestamps, restart counters or replacement validation.
func boundarySelection(state Snapshot, decisions []Task) Snapshot {
	if len(decisions) == 0 {
		return state
	}
	selection := state
	selection.Tasks = make(map[ID]Task, len(state.Tasks))
	for id, task := range state.Tasks {
		selection.Tasks[id] = task
	}
	for _, task := range decisions {
		selection.Tasks[task.ID] = task
	}
	return selection
}

func boundaryDecision(task Task, hold *BuiltinTaskHold) Task {
	metadata := BuiltinTaskExecution{Hold: hold}
	task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &metadata}
	return task
}
