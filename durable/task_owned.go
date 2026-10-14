package durable

import "context"

// OwnedTasks returns detached direct live children ordered by ID. Background
// children are included; deeper descendants and terminal tasks are excluded.
func (r *TaskRuntime) OwnedTasks(ctx context.Context) ([]TaskRecord, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	out := []TaskRecord{}
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		for _, id := range ids(state.Tasks) {
			task := state.Tasks[id]
			if task.Owner != r.taskID || terminalStatus(task.Status) {
				continue
			}
			record, err := CanonicalTask(task, r.harness.session.limits)
			if err != nil {
				return err
			}
			out = append(out, record)
		}
		return nil
	})
	return out, err
}

// AbortOwned marks a direct child and joins its actual terminal settlement.
// It uses invocation-bound waiting/capacity checks and cannot acquire authority
// over an unrelated task merely by knowing its ID.
func (r *TaskRuntime) AbortOwned(ctx context.Context, id ID) error {
	if err := r.check(); err != nil {
		return err
	}
	if ctx == nil {
		return reject("nil owned abort context")
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.context, cancel)
	defer func() { stop(); cancel() }()
	terminal := false
	_, err := r.harness.session.invocationCommit(linked, r.taskID, func(tx *Tx) error {
		if err := r.check(); err != nil {
			return err
		}
		task, ok := tx.state.Tasks[id]
		if !ok || task.Owner != r.taskID {
			return reject("task is not directly owned")
		}
		if terminalStatus(task.Status) {
			terminal = true
			return nil
		}
		if task.Execution == nil && task.Kind != "pi.generation" && task.Kind != "pi.tool" {
			return reject("legacy task has no abort adapter")
		}
		task = markTask(task)
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	if err != nil || terminal {
		return err
	}
	_, err = r.WaitForTask(linked, id)
	return err
}
