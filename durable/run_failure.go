package durable

// Record only an actual run's unanswered settlement. The native queue gate is
// separate: a continued run retains this history even after it answers.
func recordRunFailure(value JSON, run map[string]any, task Task, sub Submission, hold *BuiltinTaskHold) error {
	status := publicSubmission(sub)
	if status.Status != "unanswered" {
		return nil
	}
	if hold.Action == "scheduler-generation" && hold.Outcome.Status == "orphaned" {
		status.Reason = "orphaned"
	}
	switch status.Reason {
	case "model_error", "no_model", "aborted", "reset", "faulted", "orphaned", "interrupted", "restart_limit", "nothing_to_answer", "turn_limit":
	default:
		// Do not invent a source reason for a native-only failure code.
		return nil
	}
	inputs, ok := run["inputs"].([]any)
	if !ok {
		return reject("live run inputs unavailable")
	}
	failure := JSON{"taskId": task.ID, "inputs": append([]any{}, inputs...), "reason": status.Reason}
	if status.Detail != nil && status.Detail.Present {
		if detail, ok := status.Detail.Value.(string); ok {
			failure["detail"] = detail
		}
	} else if detail, ok := generationObject(sub.Value["message"])["errorMessage"].(string); status.Reason == "model_error" && ok {
		failure["detail"] = detail
	} else if status.Reason == "orphaned" {
		failure["detail"] = hold.Outcome.Reason
	} else if status.Reason == "model_error" || status.Reason == "no_model" || status.Reason == "faulted" || status.Reason == "interrupted" || status.Reason == "restart_limit" || status.Reason == "turn_limit" || abortHandlerCutoff(hold) {
		if hold.Outcome.Error != nil {
			failure["detail"] = hold.Outcome.Error.Message
		}
	}
	if first, exists := run["first"]; exists {
		failure["first"] = first
	}
	value["runFailure"] = failure
	return nil
}

// A scheduler abort-handler cutoff faults the task, but ends its run as aborted.
// The persisted outcome detail remains the witness after restart counters reset.
func abortHandlerCutoff(hold *BuiltinTaskHold) bool {
	return hold.Action == "scheduler-generation" && hold.Outcome.Status == "faulted" && hold.Outcome.Error != nil && hold.Outcome.Error.Detail != nil && generationObject(hold.Outcome.Error.Detail.Value)["reason"] == "restart_limit"
}

// Only inputs already queued at an automatically continued failure boundary
// keep that failure. Fresh user work starts a new run and clears its history.
func startLiveRun(tx *Tx, value JSON, task Task, cp generationCheckpoint, inputs []any) error {
	run := JSON{"task": task.ID, "inputs": inputs}
	if cp.InputEntry != 0 {
		run["first"] = cp.InputEntry
	}
	keep := false
	failure := generationObject(value["runFailure"])
	if previous, ok := tx.state.Tasks[submissionEntryID(failure["taskId"])]; ok && previous.Kind == "pi.generation" {
		var checkpoint generationCheckpoint
		if err := fromObject(previous.Checkpoint, &checkpoint, tx.limits); err != nil {
			return err
		}
		keep = (checkpoint.Reset || checkpoint.AfterFailedRun == "continue") && checkpoint.FailureCut != 0 && cp.Submission == checkpoint.FailureCut
	}
	if !keep {
		delete(value, "runFailure")
	}
	value["run"] = run
	return nil
}
