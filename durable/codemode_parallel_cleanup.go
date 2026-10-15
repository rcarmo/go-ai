package durable

import (
	"context"
	"fmt"
)

// Abort only nested calls admitted by this codemode key; other owned children
// may belong to unrelated work in the caller task.
func (r *TaskRuntime) abortCodemodeChildren(ctx context.Context, key string, journal *codemodeJournalState) error {
	snap, err := r.harness.Snapshot(ctx)
	if err != nil {
		return err
	}
	keys := map[string]bool{}
	for _, call := range journal.Issued {
		keys[fmt.Sprintf("%s.c%d", key, call.ID)] = true
	}
	for _, doc := range snap.Documents {
		if doc.Owner != r.taskID || doc.Scope != "task" || doc.Kind != nestedCallDocument || !keys[doc.Key] {
			continue
		}
		var state nestedCallState
		if err := fromObject(doc.Value, &state, r.harness.session.limits); err != nil {
			return err
		}
		child, ok := snap.Tasks[state.Task]
		if !ok || terminalStatus(child.Status) {
			continue
		}
		if err := r.AbortOwned(ctx, state.Task); err != nil {
			return err
		}
	}
	return nil
}
