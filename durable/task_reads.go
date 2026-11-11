package durable

import "context"

// canonicalStoredTask refuses legacy envelopes with no canonical input/version;
// Snapshot remains the explicit native-data inspection path for those records.
func canonicalStoredTask(task Task, limits Limits) (TaskRecord, error) {
	record, err := CanonicalTask(task, limits)
	if err != nil {
		return TaskRecord{}, err
	}
	if record.Version == 0 || record.Input == nil || !record.Input.Present {
		return TaskRecord{}, reject("legacy task has no canonical record")
	}
	return record, nil
}

// Task returns a detached canonical record, or found=false for an unused ID.
func (c *storeCore) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	if err := c.enter(ctx); err != nil {
		return TaskRecord{}, false, err
	}
	defer c.leave()
	if err := checkReadIDs(id); err != nil {
		return TaskRecord{}, false, err
	}
	task, ok := c.state.Tasks[id]
	if !ok {
		return TaskRecord{}, false, nil
	}
	record, err := canonicalStoredTask(task, c.limits)
	return record, true, err
}

func (s *Session) Task(ctx context.Context, id ID) (record TaskRecord, found bool, err error) {
	if err = s.enter(ctx); err != nil {
		return
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
		if err != nil {
			record = TaskRecord{}
			found = false
		}
	}()
	if err = checkReadIDs(id); err != nil {
		return
	}
	if err = s.touch(ctx, taskKey(id)); err != nil {
		return
	}
	if !s.hasNativeReadImage() {
		return s.guardedStorage().Task(ctx, id)
	}
	var state Snapshot
	state, err = s.taskSnapshot(ctx)
	if err != nil {
		return
	}
	task, ok := state.Tasks[id]
	found = ok
	if !ok {
		return
	}
	record, err = canonicalStoredTask(task, s.limits)
	return
}

func (t *Tx) Task(id ID) (TaskRecord, bool, error) {
	if err := t.enterTableRead("task", taskKey(id)); err != nil {
		return TaskRecord{}, false, err
	}
	defer t.leave()
	if err := checkReadIDs(id); err != nil {
		return TaskRecord{}, false, err
	}
	task, ok := t.state.Tasks[id]
	if !ok {
		return TaskRecord{}, false, nil
	}
	record, err := canonicalStoredTask(task, t.limits)
	return record, true, err
}
