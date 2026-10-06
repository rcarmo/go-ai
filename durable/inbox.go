package durable

import (
	"fmt"
	"strconv"
)

func generationIncludesSubmission(cp generationCheckpoint, id ID) bool {
	if cp.Submission == id {
		return true
	}
	for _, member := range cp.Steered {
		if member == id {
			return true
		}
	}
	return false
}

// applySteeringBoundary attaches queued steers to the current run after all tools
// actually return. Their queued placeholder task is retired without execution;
// the run's final receipt settles every attached input atomically.
func (h *Harness) applySteeringBoundary(tx *Tx, parent Task, cp *generationCheckpoint) error {
	return h.applyQueuedInputs(tx, parent, cp, false)
}

func (h *Harness) applyQueuedInputs(tx *Tx, parent Task, cp *generationCheckpoint, initial bool) error {
	if err := h.placeQueuedWrites(tx, parent.Conversation); err != nil {
		return err
	}
	settings := cp.Agent.Settings
	if doc, ok := agentDocument(tx.state, parent.Conversation); ok {
		var latest agentState
		if err := fromObject(doc.Value, &latest, tx.limits); err != nil {
			return err
		}
		settings = h.resolvedSettings(latest.Settings)
	}
	selected := map[ID]bool{}
	pickedSteer := 0
	if initial {
		if sub := tx.state.Submissions[cp.Submission]; sub.Type == "steer" {
			pickedSteer = 1
		}
	}
	for _, id := range ids(tx.state.Tasks) {
		task := tx.state.Tasks[id]
		if task.ID == parent.ID || task.Kind != "pi.generation" || task.Conversation != parent.Conversation || terminalStatus(task.Status) {
			continue
		}
		var queued generationCheckpoint
		if err := fromObject(task.Checkpoint, &queued, tx.limits); err != nil {
			return err
		}
		sub, exists := tx.state.Submissions[queued.Submission]
		if !exists || terminalStatus(sub.Status) || queued.Phase != "queued" {
			continue
		}
		if sub.Type == "steer" {
			if settings.SteeringMode != "all" && pickedSteer >= 1 {
				continue
			}
		} else if initial && sub.Type == "follow-up" && settings.FollowUpMode == "all" {
			// Every follow-up selected at the boundary shares this run.
		} else {
			continue
		}
		if h.scheduler.invocations[id] != nil || len(h.scheduler.ownedLive(tx.state, id)) > 0 {
			continue
		}
		entryID, err := tx.MintID()
		if err != nil {
			return err
		}
		input, err := inputReceipt(queued.Input, queued.InputBlocks, tx.limits)
		if err != nil {
			return err
		}
		value, err := dtoObject(input, tx.limits)
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: entryID, Conversation: parent.Conversation, Kind: "message", Value: value, ByTask: parent.ID}); err != nil {
			return err
		}
		queued.Phase = "terminal"
		queued.InputEntry = entryID
		if err := markSubmissionEntry(tx, sub.ID, entryID); err != nil {
			return err
		}
		owned, err := copyTask(task, tx.limits)
		if err != nil {
			return err
		}
		owned.Status = "done"
		owned.Checkpoint, err = dtoObject(queued, tx.limits)
		if err != nil {
			return err
		}
		if tx.taskTerminals == nil {
			tx.taskTerminals = map[ID]bool{}
		}
		tx.taskTerminals[id] = true
		if err = tx.stage(Write{Op: "put-task", Task: &owned}); err != nil {
			return err
		}
		cp.Steered = append(cp.Steered, sub.ID)
		selected[sub.ID] = true
		if sub.Type == "steer" {
			pickedSteer++
		}
	}
	if len(selected) == 0 {
		return nil
	}
	inbox, err := builtin(tx, parent.Conversation, "pi.inbox")
	if err != nil {
		return err
	}
	return inbox.Update(func(value JSON) error {
		items, ok := value["items"].([]any)
		if !ok {
			return reject("inbox shape")
		}
		keep := []any{}
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				return reject("inbox item")
			}
			if object["mode"] == "reset" {
				keep = append(keep, item)
				continue
			}
			id, err := strconv.ParseUint(fmt.Sprint(object["id"]), 10, 64)
			if err != nil {
				return err
			}
			if !selected[ID(id)] {
				keep = append(keep, item)
			}
		}
		value["items"] = keep
		return nil
	})
}
