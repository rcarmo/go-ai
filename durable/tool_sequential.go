package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

// Sequential rounds mint the next tool only after the previous actual host
// invocation is drained, matching GenerationTask.tools in the pinned runtime.
func (h *Harness) startNextSequentialTool(runtime *TaskRuntime, parent Task) error {
	_, err := h.session.invocationCommit(runtime.context, parent.ID, func(tx *Tx) error {
		if err := runtime.check(); err != nil {
			return err
		}
		current := tx.state.Tasks[parent.ID]
		if h.closing.Load() || taskAborted(current) || current.Status != "running" {
			return ErrSealed
		}
		var cp generationCheckpoint
		if err := fromObject(current.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		if cp.Phase != "tools" || !cp.Sequential || len(cp.PendingTools) == 0 {
			return reject("sequential tool boundary changed")
		}
		children, err := h.scheduler.toolRoundChildren(tx.state, current, cp)
		if err != nil {
			return err
		}
		for _, child := range children {
			if !terminalStatus(child.Status) || h.scheduler.invocations[child.ID] != nil {
				return reject("sequential previous tool still live")
			}
		}
		if len(h.scheduler.ownedLive(tx.state, parent.ID)) != 0 {
			return reject("sequential owned work still live")
		}
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		value, err := dtoObject(cp.PendingTools[0], tx.limits)
		if err != nil {
			return err
		}
		if err := tx.PutTask(Task{ID: id, Conversation: parent.Conversation, Owner: parent.ID, Kind: "pi.tool", Status: "pending", Checkpoint: value}); err != nil {
			return err
		}
		cp.PendingTools = cp.PendingTools[1:]
		cp.Children = append(cp.Children, id)
		value, err = dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		current.Checkpoint, current.Status = value, "completing"
		return tx.stage(Write{Op: "put-task", Task: &current})
	})
	return err
}

// Calls not started in a sequential round receive aborted transcript results,
// without creating tool tasks or running hooks/effects.
func (h *Harness) abortPendingSequentialTools(parent Task, cp *generationCheckpoint) error {
	_, err := h.session.invocationCommit(context.Background(), parent.ID, func(tx *Tx) error {
		current := tx.state.Tasks[parent.ID]
		var latest generationCheckpoint
		if err := fromObject(current.Checkpoint, &latest, tx.limits); err != nil {
			return err
		}
		for _, pending := range latest.PendingTools {
			diagnostic := ToolDiagnostic{Severity: "error", Code: "aborted", Message: toolErrorMessage(pending.Offer.Name, "aborted")}
			receipt := MessageReceipt{Role: "toolResult", ToolCallID: pending.CallID, ToolName: pending.Offer.Name, Content: []goai.ContentBlock{{Type: "text", Text: renderToolDiagnostics([]ToolDiagnostic{diagnostic})}}, IsError: true, ErrorCode: "aborted", Diagnostics: []ToolDiagnostic{diagnostic}, Details: JSON{}}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			value, err := dtoObject(receipt, tx.limits)
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: parent.Conversation, Kind: "message", ByTask: parent.ID, Value: value}); err != nil {
				return err
			}
		}
		latest.PendingTools = nil
		value, err := dtoObject(latest, tx.limits)
		if err != nil {
			return err
		}
		current.Checkpoint = value
		if err := tx.stage(Write{Op: "put-task", Task: &current}); err != nil {
			return err
		}
		*cp = latest
		return nil
	})
	return err
}
