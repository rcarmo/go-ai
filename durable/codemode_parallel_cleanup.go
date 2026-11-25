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
		id, err := codemodeNestedTaskID(snap, r.taskID, doc, r.harness.session.limits)
		if err != nil {
			return err
		}
		child, ok := snap.Tasks[id]
		if !ok || terminalStatus(child.Status) {
			continue
		}
		if err := r.AbortOwned(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func codemodeNestedTaskID(snap Snapshot, owner ID, doc Document, l Limits) (ID, error) {
	if canonicalToolRecord(snap.Tasks[owner]) {
		return submissionEntryID(doc.Value["taskId"]), nil
	}
	var state nestedCallState
	if err := fromObject(doc.Value, &state, l); err != nil {
		return 0, err
	}
	return state.Task, nil
}

type codemodeReplayNestedConflict struct {
	key     string
	message string
}

// The pinned host checks the held calls against the nested calls that really
// made it to storage. Preserve that order for diagnostics without dropping
// the native identity check for other held calls, including globals.
func (r *TaskRuntime) codemodeReplayConflictMessage(ctx context.Context, fallback string, conflicts []codemodeReplayNestedConflict) (string, error) {
	if len(conflicts) == 0 {
		return fallback, nil
	}
	snap, err := r.harness.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	keys := make(map[string]bool, len(conflicts))
	for _, conflict := range conflicts {
		keys[conflict.key] = true
	}
	for _, doc := range snap.Documents {
		if !keys[doc.Key] || doc.Retired || !doc.Family || doc.Scope != "task" || doc.Owner != r.taskID || doc.Kind != nestedCallDocument {
			continue
		}
		id, err := codemodeNestedTaskID(snap, r.taskID, doc, r.harness.session.limits)
		if err != nil {
			return "", err
		}
		row, found := snap.Tasks[id]
		if !found || row.Owner != r.taskID || row.Conversation != r.conversation || row.Kind != "pi.tool" && row.Kind != nestedToolKind {
			continue
		}
		delete(keys, doc.Key)
	}
	for _, conflict := range conflicts {
		if !keys[conflict.key] {
			return conflict.message, nil
		}
	}
	return fallback, nil
}
