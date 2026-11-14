package durable

import (
	"context"
)

// Reset queues a self-head marker while a generation is busy. At post-tools it
// ends the old run; at final it is placed after the answer. On an idle conversation
// it takes effect immediately. Optional text becomes the new context's user input.
func (c *ConversationHandle) Reset(ctx context.Context, text string) error {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return ErrClosed
	}
	_, err := h.session.Commit(ctx, func(tx *Tx) (any, error) {
		if err := tx.touchKeys(ALL_KEYS); err != nil {
			return nil, err
		}
		if h.closing.Load() {
			return nil, ErrClosed
		}
		if _, ok := tx.state.Conversations[c.id]; !ok {
			return nil, reject("unknown conversation")
		}
		busy := h.conversationGenerationBusy(tx.state, c.id)
		if !busy {
			return nil, appendReset(tx, c.id, text)
		}
		inbox, err := builtin(tx, c.id, "pi.inbox")
		if err != nil {
			return nil, err
		}
		return nil, inbox.Update(func(value JSON) error {
			items, ok := value["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			value["items"] = append(items, JSON{"mode": "reset", "content": text})
			return nil
		})
	})
	if err == nil {
		h.scheduler.enable()
	}
	return err
}
func appendReset(tx *Tx, conversation ID, text string) error {
	_, err := appendResetEntry(tx, conversation, text)
	return err
}
func appendResetEntry(tx *Tx, conversation ID, text string) (ID, error) {
	id, err := tx.mintID()
	if err != nil {
		return 0, err
	}
	model := []MessageReceipt{}
	if text != "" {
		model = append(model, userReceipt(text))
	}
	if err := tx.appendEntryRecord(Entry{ID: id, Conversation: conversation, Kind: "pi.reset", Head: id, Value: JSON{}, Model: model}); err != nil {
		return 0, err
	}
	return id, nil
}
func (h *Harness) hasQueuedReset(state Snapshot, conversation ID) bool {
	for _, doc := range state.Documents {
		if doc.Scope != "conversation" || doc.Owner != conversation || doc.Kind != "pi.inbox" || doc.Retired {
			continue
		}
		items, _ := doc.Value["items"].([]any)
		for _, item := range items {
			if object, ok := item.(map[string]any); ok && object["mode"] == "reset" {
				return true
			}
		}
	}
	return false
}
func (h *Harness) resetToolsRun(tx *Tx, current Task, cp generationCheckpoint) error {
	cp.Reset = true
	cp.FailureCut = nextRunSubmission(tx, current.Conversation, cp)
	// finishToolRound returns the committed assistant on a reset boundary;
	// no synthetic assistant or aborted generation outcome is introduced.
	entry, ok := tx.state.Entries[cp.AssistantEntry]
	if !ok || entry.ByTask != current.ID || entry.Conversation != current.Conversation {
		return reject("reset assistant missing")
	}
	value, err := entryMessageValue(entry, tx.limits)
	if err != nil {
		return err
	}
	cp.Phase = "terminal"
	cp.Children = nil
	current.Checkpoint, err = dtoObject(cp, tx.limits)
	if err != nil {
		return err
	}
	hold := &BuiltinTaskHold{Stage: "final", Action: "generation-reset", Outcome: TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": entry.ID}}}, FinalStatus: "done", Entry: entry.ID, Submission: cp.Submission, Conversation: current.Conversation, Owner: current.Owner}
	metadata := &BuiltinTaskExecution{}
	if current.Execution != nil {
		*metadata = *current.Execution.Builtin
	}
	metadata.Memos = nil
	metadata.Hold = hold
	current.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
	current.Status = "done"
	if len(h.scheduler.ownedLive(tx.state, current.ID)) > 0 {
		hold.Stage, current.Status = "held", "completing"
	}
	if _, err := h.cleanupGeneration(tx, &current, hold, value); err != nil {
		return err
	}
	return tx.stage(Write{Op: "put-task", Task: &current})
}
