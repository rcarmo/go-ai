package durable

import "context"

// End without another assistant entry when an already-recorded model failure
// cannot recover (for example, overflow compaction failed or was declined).
func (h *Harness) finishRecordedModelFailure(task Task, cp generationCheckpoint, message string) error {
	_, err := h.session.invocationCommit(context.Background(), task.ID, func(tx *Tx) error {
		current := tx.state.Tasks[task.ID]
		if terminalStatus(current.Status) || taskHasDecidedOutcome(current) {
			return nil
		}
		if taskAborted(current) {
			return ErrSealed
		}
		if err := fromObject(current.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		cp.Phase = "terminal"
		cp.Partial = nil
		value, err := dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		current.Checkpoint, current.Status = value, "failed"
		hold := &BuiltinTaskHold{Stage: "final", Action: "generation-model-failure", Outcome: TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: message, Detail: &TaskValue{Present: true, Value: JSON{"reason": "model_error"}}}}, FinalStatus: "failed", Submission: cp.Submission, Conversation: current.Conversation, Owner: current.Owner}
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
