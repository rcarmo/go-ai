package durable

func validAfterFailedRun(policy string) bool {
	return policy == "" || policy == "keep" || policy == "continue" || policy == "withdraw"
}
func (h *Harness) AfterFailedRun() string {
	settings := h.hostSettings.Load()
	if settings == nil {
		settings = h.options.Settings
	}
	if settings != nil && settings.AfterFailedRun != "" {
		return settings.AfterFailedRun
	}
	return "keep"
}
func failedQueueHeld(state Snapshot, conversation ID) bool {
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Scope == "conversation" && doc.Owner == conversation && doc.Kind == "pi.live" {
			held, _ := doc.Value["failedRun"].(bool)
			return held
		}
	}
	return false
}
func failurePolicyApplies(hold *BuiltinTaskHold) bool {
	if hold.FinalStatus != "failed" {
		return false
	}
	return hold.Action == "generation-receipt" || hold.Action == "generation-no-model" || hold.Action == "generation-model-failure"
}

// A failed model outcome is pinned before bounded withdrawal begins. The held
// outcome prevents cancellation, recovery or a changed host policy from replaying
// the model or changing its decision. Inputs settle only after withdrawal finishes.
func (h *Harness) prepareFailedCleanup(tx *Tx, task *Task, hold *BuiltinTaskHold, cp *generationCheckpoint) (bool, error) {
	if !failurePolicyApplies(hold) {
		return false, nil
	}
	if !validAfterFailedRun(cp.AfterFailedRun) {
		return false, reject("invalid failed-run policy")
	}
	if !taskHasDecidedOutcome(tx.state.Tasks[task.ID]) {
		h.pinFailedRunPolicy(tx, task.Conversation, cp)
		checkpoint, err := dtoObject(*cp, tx.limits)
		if err != nil {
			return false, err
		}
		task.Checkpoint = checkpoint
		if cp.AfterFailedRun == "withdraw" {
			// Always adopt the small decision first, not an unbounded finish batch.
			hold.Stage, task.Status = "held", "completing"
			return true, nil
		}
	}
	if cp.AfterFailedRun == "withdraw" {
		withdrew, err := h.withdrawFailedInput(tx, *task, *cp)
		if err != nil {
			return false, err
		}
		if withdrew {
			hold.Stage, task.Status = "held", "completing"
			return true, nil
		}
	}
	return false, nil
}

// One constant-size cleanup unit. Only unplaced inputs at the pinned boundary
// are selected; writes and post-decision inputs are never withdrawn.
func (h *Harness) pinFailedRunPolicy(tx *Tx, conversation ID, cp *generationCheckpoint) {
	if cp.AfterFailedRun != "" {
		return
	}
	cp.AfterFailedRun = h.AfterFailedRun()
	for id, sub := range tx.state.Submissions {
		if sub.Conversation == conversation && id > cp.FailureCut {
			cp.FailureCut = id
		}
	}
}

func (h *Harness) withdrawFailedInput(tx *Tx, failed Task, cp generationCheckpoint) (bool, error) {
	for _, id := range ids(tx.state.Tasks) {
		task := tx.state.Tasks[id]
		if task.ID == failed.ID || task.Conversation != failed.Conversation || task.Kind != "pi.generation" || terminalStatus(task.Status) {
			continue
		}
		var queued generationCheckpoint
		if err := fromObject(task.Checkpoint, &queued, tx.limits); err != nil {
			return false, err
		}
		sub, ok := tx.state.Submissions[queued.Submission]
		if !ok {
			return false, reject("queued submission missing")
		}
		if queued.Phase != "queued" || sub.ID > cp.FailureCut || terminalStatus(sub.Status) || sub.Type != "steer" && sub.Type != "follow-up" || submissionEntryID(sub.Value["placedEntry"]) != 0 {
			continue
		}
		queued.Abort = true
		queued.Phase = "terminal"
		checkpoint, err := dtoObject(queued, tx.limits)
		if err != nil {
			return false, err
		}
		task.Status, task.Checkpoint = "failed", checkpoint
		hold := &BuiltinTaskHold{Stage: "final", Action: "generation-withdraw", FinalStatus: "failed", Outcome: TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "run_failed"}}, Submission: sub.ID, Conversation: task.Conversation, Owner: task.Owner}
		if len(h.scheduler.ownedLive(tx.state, id)) != 0 {
			hold.Stage, task.Status = "held", "completing"
		}
		task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{AbortRequested: true, Hold: hold}}
		if runtime := h.scheduler.invocations[id]; runtime != nil {
			tx.taskStops = append(tx.taskStops, taskStop{runtime: runtime})
		}
		if err = tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
			return false, err
		}
		sub.Status = "failed"
		sub.Value = JSON{"errorCode": "run_failed"}
		if err = tx.PutSubmission(sub); err != nil {
			return false, err
		}
		inbox, err := builtin(tx, failed.Conversation, "pi.inbox")
		if err != nil {
			return false, err
		}
		err = inbox.Update(func(value JSON) error {
			items, ok := value["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			keep := make([]any, 0, len(items))
			for _, raw := range items {
				item, ok := raw.(map[string]any)
				if !ok {
					return reject("inbox item")
				}
				if submissionEntryID(item["id"]) != sub.ID {
					keep = append(keep, raw)
				}
			}
			value["items"] = keep
			return nil
		})
		return true, err
	}
	return false, nil
}

// Kept queued placeholders are idle, but never hide actual hosts or children.
func (s *taskScheduler) heldFailedInput(state Snapshot, task Task) bool {
	if task.Kind != "pi.generation" || terminalStatus(task.Status) || taskHasDecidedOutcome(task) || taskAborted(task) || s.invocations[task.ID] != nil || len(s.ownedLive(state, task.ID)) != 0 || !failedQueueHeld(state, task.Conversation) {
		return false
	}
	phase, _ := task.Checkpoint["phase"].(string)
	return phase == "queued"
}
