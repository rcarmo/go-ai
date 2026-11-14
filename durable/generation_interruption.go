package durable

import (
	"fmt"

	goai "github.com/rcarmo/go-ai"
)

// RetryCount counts logical failures, not dispatches: Attempt also advances on
// a clean resend and must remain available for presentation ownership fences.
// Commit the charge, partial conversion and next state together. A crash before
// that commit leaves the original intent; after it, phase retry/terminal cannot
// charge the same interruption again.
func (h *Harness) chargeInterruptedGeneration(runtime *TaskRuntime, task Task, cp generationCheckpoint) error {
	settings := cp.Agent.Settings
	if err := h.session.readTasks(runtime.context, func(state Snapshot) error {
		if doc, ok := agentDocument(state, task.Conversation); ok {
			var agent agentState
			if err := fromObject(doc.Value, &agent, h.session.limits); err != nil {
				return err
			}
			settings = h.resolvedSettings(agent.Settings)
		}
		return nil
	}); err != nil {
		return err
	}
	location := "the model was answering"
	if cp.Phase == "poll" {
		location = "the deferred answer was fetched"
	}
	message := fmt.Sprintf("The host stopped while %s, on attempt %d", location, cp.RetryCount+1)
	retry := settings.Retry.Enabled && cp.RetryCount < settings.Retry.MaxRetries
	if !retry {
		// Provider callbacks stay outside the Session line, with the invocation
		// context and existing best-effort cancellation/reporting fences.
		h.cancelDeferred(runtime.context, cp, runtime)
	}
	now, err := runtime.Now()
	if err != nil {
		return err
	}
	_, err = h.session.invocationCommit(runtime.context, task.ID, func(tx *Tx) error {
		current := tx.state.Tasks[task.ID]
		if runtime.ended.Load() || h.closing.Load() || current.Status != "running" || taskHasDecidedOutcome(current) || taskAborted(current) {
			return ErrSealed
		}
		var latest generationCheckpoint
		if err := fromObject(current.Checkpoint, &latest, tx.limits); err != nil {
			return err
		}
		if current.Restarts == 0 || latest.Phase != cp.Phase || latest.RetryCount != cp.RetryCount {
			return ErrSealed
		}
		if _, err := recordInterruptedGenerationPartial(tx, current, &latest); err != nil {
			return err
		}
		if !retry {
			return h.stageRecordedGenerationFailure(tx, current, latest, message, "interrupted")
		}
		latest.RetryCount++
		latest.Phase, latest.RetryMessage = "retry", message
		latest.RetryUntil = retryDeadline(now, retryDelay(settings.Retry, latest.RetryCount))
		checkpoint, err := dtoObject(latest, tx.limits)
		if err != nil {
			return err
		}
		owned, err := copyTask(current, tx.limits)
		if err != nil {
			return err
		}
		owned.Checkpoint, owned.Status = checkpoint, "pending"
		sleepGeneration(&owned)
		return tx.stage(Write{Op: "put-task", Task: &owned})
	})
	return err
}

func recordInterruptedGenerationPartial(tx *Tx, current Task, cp *generationCheckpoint) (bool, error) {
	partial, err := generationPartial(tx.state, current, *cp, tx.limits)
	if err != nil || partial == nil {
		return false, err
	}
	receipt := *partial
	receipt.StopReason = goai.StopReasonAborted
	id, err := tx.mintID()
	if err != nil {
		return false, err
	}
	value, err := dtoObject(receipt, tx.limits)
	if err != nil {
		return false, err
	}
	if err := tx.appendMessage(id, current.Conversation, current.ID, value); err != nil {
		return false, err
	}
	usage, err := builtin(tx, current.Conversation, "pi.usage")
	if err != nil {
		return false, err
	}
	if err := usage.Update(func(value JSON) error { return addModelUsage(value, receipt, tx.limits) }); err != nil {
		return false, err
	}
	cp.Partial = nil
	if err := clearGenerationPresentation(tx, current.Conversation); err != nil {
		return false, err
	}
	return true, nil
}
