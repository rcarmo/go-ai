package durable

import (
	"context"
	"fmt"
)

type generationNoModel struct{ ref ModelRef }

func (e *generationNoModel) Error() string {
	if e.ref.ID == "" {
		return "No model is configured"
	}
	return fmt.Sprintf("Model %s/%s is not available", e.ref.Provider, e.ref.ID)
}

func (h *Harness) finishGenerationNoModel(task Task, cp generationCheckpoint, cause *generationNoModel) error {
	_, err := h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		current := tx.state.Tasks[task.ID]
		if terminalStatus(current.Status) || taskHasDecidedOutcome(current) {
			return nil
		}
		if err := fromObject(current.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		if taskAborted(current) {
			return ErrSealed
		}
		if cp.Phase == "queued" && cp.InputEntry == 0 {
			input, err := inputReceipt(cp.Input, cp.InputBlocks, tx.limits)
			if err != nil {
				return err
			}
			value, err := dtoObject(input, tx.limits)
			if err != nil {
				return err
			}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: current.Conversation, Kind: "message", Value: value, ByTask: current.ID}); err != nil {
				return err
			}
			cp.InputEntry = id
			if err := markSubmissionEntry(tx, cp.Submission, id); err != nil {
				return err
			}
		}
		if cp.Phase == "queued" {
			if err := h.applyQueuedInputs(tx, current, &cp, true); err != nil {
				return err
			}
		}
		cp.Phase = "terminal"
		cp.Partial = nil
		value, err := dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		current.Checkpoint, current.Status = value, "failed"
		hold := &BuiltinTaskHold{Stage: "final", Action: "generation-no-model", Outcome: TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: cause.Error(), Detail: &TaskValue{Present: true, Value: JSON{"reason": "no_model"}}}}, FinalStatus: "failed", Submission: cp.Submission, Conversation: current.Conversation, Owner: current.Owner}
		current.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{Hold: hold}}
		if len(h.scheduler.ownedLive(tx.state, current.ID)) > 0 {
			hold.Stage, current.Status = "held", "completing"
		}
		if err := h.cleanupGeneration(tx, current, hold, nil); err != nil {
			return err
		}
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err
}
