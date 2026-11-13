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

type generationNothingToAnswer struct{}

func (*generationNothingToAnswer) Error() string { return "The conversation has nothing to answer" }

func (h *Harness) finishGenerationNoModel(task Task, cp generationCheckpoint, cause *generationNoModel) error {
	return h.finishGenerationWithoutRequest(task, cp, "no_model", cause.Error())
}

func (h *Harness) finishGenerationWithoutRequest(task Task, cp generationCheckpoint, reason, message string) error {
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
		if cp.Phase == "queued" && cp.InputEntry == 0 && tx.state.Submissions[cp.Submission].Type != "continue" {
			id, err := appendQueuedInput(tx, current.Conversation, cp.Submission, h.session.now(), current.ID)
			if err != nil {
				return err
			}
			cp.InputEntry = id
			if err := markSubmissionEntry(tx, cp.Submission, id); err != nil {
				return err
			}
		}
		// End this run before the failed-run boundary selects queued users.
		// They belong to the successor, not to this no-model settlement.
		h.pinFailedRunPolicy(tx, current.Conversation, &cp)
		// A missing model can fail before request preparation establishes live.run.
		// Use its already placed input and attached inputs, never a synthetic ID.
		if cp.InputEntry != 0 {
			live, err := builtin(tx, task.Conversation, "pi.live")
			if err != nil {
				return err
			}
			if err := live.Update(func(value JSON) error {
				run := generationObject(value["run"])
				if len(run) != 0 {
					if submissionEntryID(run["task"]) != task.ID {
						return reject("generation run ownership changed")
					}
					return nil
				}
				inputs := []any{cp.Submission}
				for _, input := range cp.Steered {
					inputs = append(inputs, input)
				}
				return startLiveRun(tx, value, task, cp, inputs)
			}); err != nil {
				return err
			}
		}
		if reason == "restart_limit" && task.Restarts <= h.RestartLimit() {
			if _, err := recordInterruptedGenerationPartial(tx, current, &cp); err != nil {
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
		action := "generation-no-model"
		if reason == "nothing_to_answer" {
			action = "generation-no-answer"
		} else if reason == "restart_limit" {
			action = "generation-model-failure"
		}
		hold := &BuiltinTaskHold{Stage: "final", Action: action, Outcome: TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: message, Detail: &TaskValue{Present: true, Value: JSON{"reason": reason}}}}, FinalStatus: "failed", Submission: cp.Submission, Conversation: current.Conversation, Owner: current.Owner}
		current.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{Hold: hold}}
		if len(h.scheduler.ownedLive(tx.state, current.ID)) > 0 {
			hold.Stage, current.Status = "held", "completing"
		}
		if _, err := h.cleanupGeneration(tx, &current, hold, nil); err != nil {
			return err
		}
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err
}
