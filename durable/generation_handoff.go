package durable

// handoffToolGeneration is one committed boundary: completed tool-calling
// generation plus conversation-owned successor and pi.live run transfer. Inputs
// remain pending and spent assistant/tool receipts are never emitted again.
func (h *Harness) handoffToolGeneration(tx *Tx, current Task, cp generationCheckpoint) (Task, generationCheckpoint, error) {
	if cp.AssistantEntry == 0 {
		return Task{}, generationCheckpoint{}, reject("generation handoff unavailable")
	}
	entry, ok := tx.state.Entries[cp.AssistantEntry]
	// onYield appends its assistant in this same transaction. Read only that
	// staged entry; an empty tools-boundary write list is not a commit candidate.
	for _, write := range tx.writes {
		if write.Entry != nil && write.Entry.ID == cp.AssistantEntry {
			entry, ok = *write.Entry, true
		}
	}
	if !ok || entry.Conversation != current.Conversation || entry.ByTask != current.ID {
		return Task{}, generationCheckpoint{}, reject("generation handoff assistant missing")
	}
	id, err := tx.MintID()
	if err != nil {
		return Task{}, generationCheckpoint{}, err
	}
	// Reference createGeneration starts a fresh request. Persist only run inputs;
	// preparation reads the committed agent/context anew. Do not carry old model,
	// request transcript, offers, retries, compaction or tool counters forward.
	next := generationCheckpoint{Phase: "prepare-next", Submission: cp.Submission,
		Input: cp.Input, InputBlocks: cp.InputBlocks, InputEntry: cp.InputEntry, Steered: cp.Steered}
	encoded, err := dtoObject(next, tx.limits)
	if err != nil {
		return Task{}, generationCheckpoint{}, err
	}
	successor := Task{ID: id, Conversation: current.Conversation, Kind: "pi.generation", Status: "pending", Checkpoint: encoded}
	if err := tx.stage(Write{Op: "put-task", Task: &successor}); err != nil {
		return Task{}, generationCheckpoint{}, err
	}
	cp.Phase, cp.Successor = "terminal", id
	// Keep the completed round's children for historical ownership/replay proofs.
	encoded, err = dtoObject(cp, tx.limits)
	if err != nil {
		return Task{}, generationCheckpoint{}, err
	}
	current.Checkpoint, current.Status = encoded, "done"
	metadata := &BuiltinTaskExecution{}
	if current.Execution != nil {
		*metadata = *current.Execution.Builtin
	}
	metadata.Memos = nil
	stage := "final"
	if len(h.scheduler.ownedLive(tx.state, current.ID)) > 0 {
		stage, current.Status = "held", "completing"
	}
	metadata.Hold = &BuiltinTaskHold{Stage: stage, Action: "generation-handoff", Outcome: TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": entry.ID}}}, FinalStatus: "done", Entry: entry.ID, Submission: cp.Submission, Conversation: current.Conversation, Owner: current.Owner}
	current.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
	if err := tx.stage(Write{Op: "put-task", Task: &current}); err != nil {
		return Task{}, generationCheckpoint{}, err
	}
	live, err := builtin(tx, current.Conversation, "pi.live")
	if err != nil {
		return Task{}, generationCheckpoint{}, err
	}
	if err := live.Update(func(value JSON) error {
		inputs := []any{cp.Submission}
		for _, steer := range cp.Steered {
			inputs = append(inputs, steer)
		}
		value["run"] = JSON{"task": id, "inputs": inputs}
		delete(value, "tools")
		return nil
	}); err != nil {
		return Task{}, generationCheckpoint{}, err
	}
	return successor, next, nil
}
