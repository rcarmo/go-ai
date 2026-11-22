package durable

import (
	"context"

	goai "github.com/rcarmo/go-ai"
)

// A legacy queued submission may still own a generation task. Settle that
// task in the same transaction as the inbox update, without running it or
// manufacturing an assistant entry.
func settleQueuedGenerationAbort(tx *Tx, task Task, cp generationCheckpoint) error {
	cp.Phase = "terminal"
	checkpoint, err := dtoObject(cp, tx.limits)
	if err != nil {
		return err
	}
	task.Checkpoint, task.Status = checkpoint, "aborted"
	task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{
		AbortRequested: cp.Abort || taskAborted(task),
		Hold: &BuiltinTaskHold{
			Stage: "final", Action: "generation-abort", FinalStatus: "aborted",
			Outcome: TaskOutcome{Status: "aborted"}, Submission: cp.Submission,
			Conversation: task.Conversation, Owner: task.Owner,
		},
	}}
	return tx.stage(Write{Op: "put-task", Task: &task})
}

// Abort ends the run without inventing an assistant error. Only a committed
// partial is converted to an aborted assistant entry, matching convertPartial.
func (h *Harness) finishAbortedGeneration(parent Task, cp generationCheckpoint) error {
	_, err := h.session.invocationCommit(context.Background(), parent.ID, func(tx *Tx) error {
		current := tx.state.Tasks[parent.ID]
		if terminalStatus(current.Status) || taskHasDecidedOutcome(current) {
			return nil
		}
		var latest generationCheckpoint
		if err := fromObject(current.Checkpoint, &latest, tx.limits); err != nil {
			return err
		}
		cp = latest
		partial, err := generationPartial(tx.state, current, cp, tx.limits)
		if err != nil {
			return err
		}
		var entry ID
		var value JSON
		if partial != nil {
			receipt := *partial
			receipt.StopReason = goai.StopReasonAborted
			var err error
			value, err = dtoObject(receipt, tx.limits)
			if err != nil {
				return err
			}
			entry, err = tx.mintID()
			if err != nil {
				return err
			}
			if err := tx.appendMessage(entry, current.Conversation, current.ID, value); err != nil {
				return err
			}
			usage, err := builtin(tx, current.Conversation, "pi.usage")
			if err != nil {
				return err
			}
			if err := usage.Update(func(value JSON) error { return addModelUsage(value, receipt, tx.limits) }); err != nil {
				return err
			}
		}
		cp.Phase, cp.Partial, cp.Abort = "terminal", nil, true
		checkpoint, err := dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		current.Checkpoint, current.Status = checkpoint, "aborted"
		hold := &BuiltinTaskHold{Stage: "final", Action: "generation-abort", Outcome: TaskOutcome{Status: "aborted"}, FinalStatus: "aborted", Entry: entry, Submission: cp.Submission, Conversation: current.Conversation, Owner: current.Owner}
		metadata := &BuiltinTaskExecution{AbortRequested: true, Hold: hold}
		if current.Execution != nil {
			metadata.Memos = current.Execution.Builtin.Memos
		}
		metadata.Memos = nil
		current.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
		if len(h.scheduler.ownedLive(tx.state, current.ID)) > 0 {
			hold.Stage, current.Status = "held", "completing"
		}
		if _, err := h.cleanupGeneration(tx, &current, hold, value); err != nil {
			return err
		}
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err
}
