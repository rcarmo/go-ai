package durable

// Source live.ts settleSchedulerOutcome: scheduler-written outcomes clean up
// only at the terminal transition, after ordinary owned work has ended.
func canonicalSchedulerOutcome(tx *Tx, task Task, outcome TaskOutcome) error {
	if outcome.Status != "faulted" && outcome.Status != "orphaned" {
		return nil
	}
	if task.Kind != "pi.generation" && task.Kind != "pi.tool" && task.Kind != "pi.compaction" {
		return nil
	}
	doc, err := builtin(tx, task.Conversation, "pi.live")
	if err != nil {
		return err
	}
	live, err := doc.Get()
	if err != nil {
		return err
	}
	cutOff := false
	if outcome.Error != nil && outcome.Error.Detail != nil {
		cutOff = generationObject(outcome.Error.Detail.Value)["reason"] == "restart_limit"
	}
	if task.Kind == "pi.tool" {
		for _, key := range []string{"tools", "nestedTools"} {
			items, _ := live[key].([]any)
			for _, raw := range items {
				slot := generationObject(raw)
				if submissionEntryID(slot["taskId"]) == task.ID {
					slot["status"] = "done"
					for _, field := range []string{"output", "droppedBytes", "droppedLines", "details", "diagnostics"} {
						delete(slot, field)
					}
				}
			}
		}
		removed := map[ID]bool{task.ID: true}
		items, _ := live["nestedTools"].([]any)
		kept := []any{}
		for _, raw := range items {
			slot := generationObject(raw)
			if removed[submissionEntryID(slot["parentTaskId"])] {
				removed[submissionEntryID(slot["taskId"])] = true
			} else {
				kept = append(kept, raw)
			}
		}
		if len(kept) == 0 {
			delete(live, "nestedTools")
		} else {
			live["nestedTools"] = kept
		}
	} else if task.Kind == "pi.compaction" {
		items, _ := live["compactions"].([]any)
		kept := []any{}
		for _, raw := range items {
			slot := generationObject(raw)
			if submissionEntryID(slot["taskId"]) != task.ID {
				kept = append(kept, raw)
				continue
			}
			if !cutOff {
				message := outcome.Reason
				if outcome.Error != nil {
					message = outcome.Error.Message
				}
				live["compactionFailure"] = JSON{"taskId": task.ID, "trigger": slot["trigger"], "error": message}
			}
		}
		if len(kept) == 0 {
			delete(live, "compactions")
		} else {
			live["compactions"] = kept
		}
	} else {
		if submissionEntryID(generationObject(live["run"])["taskId"]) != task.ID {
			return nil
		}
		if !cutOff {
			if err := canonicalConvertPartial(tx, task.Conversation, live); err != nil {
				return err
			}
		}
		reason, detail := "orphaned", outcome.Reason
		var override []SubmissionSettlement
		if outcome.Status == "faulted" {
			reason = "faulted"
			if cutOff {
				reason = "aborted"
			}
			detail = outcome.Error.Message
		} else {
			override = []SubmissionSettlement{{Status: "unanswered", Reason: outcome.Reason}}
		}
		if err := canonicalEndRun(tx, live, task.ID, 0, reason, &detail, override...); err != nil {
			return err
		}
	}
	return doc.Set(live)
}
