package durable

// Nested progress shares bounded output/decoder policy with model tools, but
// persists in the native execution checkpoint instead of a pi.tool envelope.
// Read the latest checkpoint inside Commit so concurrent memo/state writes are
// not overwritten. Admission and abort checks precede each progress mutation.
func (a *ToolAPI) updateNestedProgress(change func(*toolCheckpoint, Limits) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return ErrSealed
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	decoder := a.outputDecoder
	candidate := a.checkpoint
	err := a.runtime.Commit(a.ctx, func(_ *Tx, record TaskRecord) (*TaskState, error) {
		cp := record.State.Checkpoint
		if value, ok := cp["progress"].(map[string]any); ok {
			if err := fromObject(JSON(value), &candidate, a.h.session.limits); err != nil {
				return nil, err
			}
		}
		if err := change(&candidate, a.h.session.limits); err != nil {
			return nil, err
		}
		object, err := dtoObject(candidate, a.h.session.limits)
		if err != nil {
			return nil, err
		}
		cp["progress"] = object
		return &TaskState{Status: "running", Checkpoint: cp}, nil
	})
	if err != nil {
		a.outputDecoder = decoder
		return err
	}
	a.checkpoint = candidate
	return nil
}
