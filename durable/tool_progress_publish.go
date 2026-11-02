package durable

import "strings"

func copyToolProgress(to *toolCheckpoint, from toolCheckpoint) {
	to.Output = from.Output
	to.Details = from.Details
	to.HasDetails = from.HasDetails
	to.DroppedBytes, to.DroppedLines = from.DroppedBytes, from.DroppedLines
	to.OutputRaw, to.OutputFull = from.OutputRaw, from.OutputFull
	to.OutputBytes, to.OutputNewlines, to.OutputTerminated = from.OutputBytes, from.OutputNewlines, from.OutputTerminated
	to.Diagnostics = from.Diagnostics
}
func (a *ToolAPI) publishProgress() (int, error) {
	a.mu.Lock()
	value, err := dtoObject(a.checkpoint, a.h.session.limits)
	a.mu.Unlock()
	if err != nil {
		return 0, err
	}
	var snapshot toolCheckpoint
	if err := fromObject(value, &snapshot, a.h.session.limits); err != nil {
		return 0, err
	}
	written := 0
	err = a.runtime.commitProgress(a.runtime.context, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		return nil, a.writeProgress(tx, snapshot, value, &written)
	})
	return written, err
}
func (a *ToolAPI) writeProgress(tx *Tx, snapshot toolCheckpoint, value JSON, written *int) error {
	if err := a.runtime.check(); err != nil {
		return err
	}
	if a.h.closing.Load() {
		return ErrClosed
	}
	current, ok := tx.state.Tasks[a.runtime.TaskID()]
	if !ok || current.Status != "running" || taskHasDecidedOutcome(current) || taskAborted(current) {
		return ErrSealed
	}
	var candidate toolCheckpoint
	if err := fromObject(current.Checkpoint, &candidate, tx.limits); err != nil {
		return err
	}
	candidate, err := toolPresentation(tx.state, current, candidate, tx.limits)
	if err != nil {
		return err
	}
	if snapshot.Output != candidate.Output {
		if strings.HasPrefix(snapshot.Output, candidate.Output) {
			*written += len(snapshot.Output) - len(candidate.Output)
		} else {
			*written += len(snapshot.Output)
		}
	}
	if snapshot.HasDetails != candidate.HasDetails || !equalJSONValue(snapshot.Details, candidate.Details) {
		encoded, err := encodeBounded(snapshot.Details, tx.limits, tx.limits.MaxDocumentBytes)
		if err != nil {
			return err
		}
		*written += len(encoded)
	}
	if len(snapshot.Diagnostics) > len(candidate.Diagnostics) {
		encoded, err := encodeBounded(snapshot.Diagnostics[len(candidate.Diagnostics):], tx.limits, tx.limits.MaxDocumentBytes)
		if err != nil {
			return err
		}
		*written += len(encoded)
	}
	live, err := builtin(tx, current.Conversation, "pi.live")
	if err != nil {
		return err
	}
	return live.Update(func(document JSON) error {
		slot := toolPresentationSlot(document, current, candidate.CallID)
		if slot == nil {
			return ErrSealed
		}
		setToolPresentation(slot, value, snapshot.HasDetails)
		return nil
	})
}
