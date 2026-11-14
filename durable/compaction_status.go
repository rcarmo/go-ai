package durable

// syncCompactionStatuses runs in transaction finalisation, including scheduler
// faults/abort and task retirement. It uses private staging after public Tx seal.
func (t *Tx) syncCompactionStatuses() error {
	changed := map[ID]Task{}
	for _, write := range t.writes {
		if write.Task != nil && write.Task.Kind == "pi.compaction" {
			changed[write.Task.ID] = *write.Task
		}
	}
	for _, id := range ids(changed) {
		task := changed[id]
		var live Document
		for _, document := range t.state.Documents {
			if document.Scope == "conversation" && document.Owner == task.Conversation && document.Kind == "pi.live" && !document.Retired {
				live = document
				break
			}
		}
		// Public harness compaction creates pi.live before admission. Raw task-only
		// transactions without builtin documents do not fabricate a harness surface.
		if live.ID == 0 {
			continue
		}
		current, err := t.currentDocument(live.ID)
		if err != nil {
			return err
		}
		value, err := copyObject(current.Value, t.limits)
		if err != nil {
			return err
		}
		statuses, _ := value["compactions"].([]any)
		next := make([]any, 0, len(statuses)+1)
		var previous map[string]any
		for _, raw := range statuses {
			object, ok := raw.(map[string]any)
			if !ok {
				return reject("compaction status shape")
			}
			number, ok := exactNumber(object["taskId"])
			if !ok {
				return reject("compaction status identity")
			}
			if ID(number.Num().Uint64()) != id {
				next = append(next, raw)
			} else {
				previous = object
			}
		}
		if !terminalStatus(task.Status) && !taskHasDecidedOutcome(task) {
			record, err := CanonicalTask(task, t.limits)
			if err != nil {
				return err
			}
			reason := "threshold"
			if record.Input != nil {
				if input, ok := record.Input.Value.(map[string]any); ok {
					if text, ok := input["reason"].(string); ok {
						reason = text
					}
				}
			}
			attempt := 1
			checkpoint := taskCheckpointMap(record.State.Checkpoint)
			if raw, ok := checkpoint["attempt"]; ok {
				if number, ok := exactNumber(raw); ok {
					attempt = int(number.Num().Int64())
				}
			}
			status := JSON{"taskId": id, "trigger": reason, "blocking": task.Owner != 0, "attempt": attempt}
			if checkpoint["phase"] == "retry" {
				if previous != nil {
					if retry, present := previous["retry"]; present {
						status["retry"] = retry
					}
				}
			}
			next = append(next, status)
		}
		if terminalStatus(task.Status) || taskHasDecidedOutcome(task) {
			record, err := CanonicalTask(task, t.limits)
			if err != nil {
				return err
			}
			if outcome := record.State.Outcome; outcome != nil {
				if outcome.Status == "failed" || outcome.Status == "faulted" || outcome.Status == "orphaned" {
					message := "Compaction failed"
					if outcome.Error != nil {
						message = outcome.Error.Message
					}
					reason, _ := task.Checkpoint["reason"].(string)
					if record.Input != nil {
						if input, ok := record.Input.Value.(map[string]any); ok {
							if captured, ok := input["reason"].(string); ok {
								reason = captured
							}
						}
					}
					if reason == "" {
						reason = "threshold"
					}
					value["compactionFailure"] = map[string]any{"taskId": task.ID, "trigger": reason, "error": message}
				}
			}
		}
		if len(next) == 0 {
			delete(value, "compactions")
		} else {
			value["compactions"] = next
		}
		if equalJSONValue(value, current.Value) {
			continue
		}
		current.Value = value
		current.DeltasSinceBase = 0
		if err := t.stage(Write{Op: "put-document", Document: &current}); err != nil {
			return err
		}
	}
	return nil
}

func compactionFailureFromLive(live JSON) *CompactionFailure {
	raw, ok := live["compactionFailure"].(map[string]any)
	if !ok {
		return nil
	}
	number, ok := exactNumber(raw["taskId"])
	if !ok || !number.IsInt() || !number.Num().IsUint64() || number.Num().Uint64() == 0 || number.Num().Uint64() > MaxID {
		return nil
	}
	trigger, _ := raw["trigger"].(string)
	message, _ := raw["error"].(string)
	return &CompactionFailure{Task: ID(number.Num().Uint64()), Trigger: trigger, Error: message}
}

// Retry diagnostics belong to the LiveDoc status, not SummaryRequest.
func setCompactionRetryStatus(tx *Tx, conversation, task ID, until int64, message string) error {
	live, err := builtin(tx, conversation, "pi.live")
	if err != nil {
		return err
	}
	return live.Update(func(value JSON) error {
		statuses, _ := value["compactions"].([]any)
		for _, raw := range statuses {
			status, ok := raw.(map[string]any)
			if !ok {
				return reject("compaction status shape")
			}
			if submissionEntryID(status["taskId"]) == task {
				status["retry"] = JSON{"at": until, "error": message}
				break
			}
		}
		return nil
	})
}
