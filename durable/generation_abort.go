package durable

import (
	"context"

	goai "github.com/rcarmo/go-ai"
)

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
		var entry ID
		var value JSON
		if cp.Partial != nil {
			receipt := *cp.Partial
			receipt.StopReason = goai.StopReasonAborted
			var err error
			value, err = dtoObject(receipt, tx.limits)
			if err != nil {
				return err
			}
			entry, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: entry, Conversation: current.Conversation, Kind: "message", Value: value, ByTask: current.ID}); err != nil {
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
		if err := h.cleanupGeneration(tx, current, hold, value); err != nil {
			return err
		}
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err
}
