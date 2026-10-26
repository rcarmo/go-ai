package durable

// Generation's original checkpoint remains the sole authority for effects and
// pacing. Project its scalar deadline without decoding retained transcripts.
func generationPacingDeadline(task Task) (int64, bool) {
	if task.Kind != "pi.generation" {
		return 0, false
	}
	key := ""
	switch task.Checkpoint["phase"] {
	case "retry":
		key = "retryUntil"
	case "poll":
		if task.Checkpoint["deferred"] == nil {
			return 0, false
		}
		key = "pollAt"
	default:
		return 0, false
	}
	value, present := task.Checkpoint[key]
	if !present {
		return 0, true
	} // typed checkpoints omit a zero millisecond value
	number, ok := exactNumber(value)
	if !ok || !number.IsInt() || !number.Num().IsInt64() {
		return 0, false
	}
	return number.Num().Int64(), true
}
func taskSleepDeadline(task Task) (int64, bool) {
	if task.Status != "sleeping" || task.Execution == nil {
		return 0, false
	}
	if native := task.Execution.Native; native != nil && native.State.Status == "sleeping" && native.State.Until != nil {
		return *native.State.Until, true
	}
	if builtin := task.Execution.Builtin; builtin != nil && builtin.Hold == nil {
		return generationPacingDeadline(task)
	}
	return 0, false
}
func sleepGeneration(task *Task) bool {
	until, ok := generationPacingDeadline(*task)
	if !ok || until < 0 || uint64(until) > MaxID {
		return false
	}
	metadata := &BuiltinTaskExecution{}
	if task.Execution != nil {
		if task.Execution.Builtin == nil || task.Execution.Builtin.Hold != nil {
			return false
		}
		*metadata = *task.Execution.Builtin
	}
	task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
	task.Status = "sleeping"
	return true
}

// Yield old running/pending retry/poll checkpoints and clock rollbacks. A due
// continuation never retains a permit while waiting for an ordinary deadline.
// Preserve the legacy signed-int64 wait only outside public sleep's range.
func (r *TaskRuntime) waitGenerationDeadline(until int64) (bool, error) {
	now, err := r.Now()
	if err != nil {
		return false, err
	}
	if now >= until {
		return false, nil
	}
	if until < 0 || uint64(until) > MaxID {
		return false, r.waitUntil(r.context, until)
	}
	_, err = r.harness.session.invocationCommit(r.context, r.taskID, func(tx *Tx) error {
		current, ok := tx.state.Tasks[r.taskID]
		if !ok || current.Status != "running" || r.ended.Load() || taskAborted(current) || taskHasDecidedOutcome(current) || r.harness.closing.Load() {
			return ErrSealed
		}
		current, err := copyTask(current, tx.limits)
		if err != nil {
			return err
		}
		deadline, valid := generationPacingDeadline(current)
		if !valid || deadline != until || !sleepGeneration(&current) {
			return ErrSealed
		}
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err == nil, err
}
