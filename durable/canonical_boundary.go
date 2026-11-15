package durable

// Source inbox.ts boundary selection. It reads the committed head before the
// first table write and operates on canonical Inbox items, never placeholder tasks.
type canonicalBoundary struct {
	conversation ID
	inbox        *DocumentHandle
	items        []any
	head         ID
	settings     RequestSettings
}

func prepareCanonicalBoundary(tx *Tx, conversation ID, settings RequestSettings) (*canonicalBoundary, error) {
	marker, found, err := tx.LatestHeadMarker(conversation)
	if err != nil {
		return nil, err
	}
	head := ID(0)
	if found {
		head = marker.Head
	}
	inbox, err := builtin(tx, conversation, "pi.inbox")
	if err != nil {
		return nil, err
	}
	value, err := inbox.Get()
	if err != nil {
		return nil, err
	}
	items, _ := value["items"].([]any)
	return &canonicalBoundary{conversation: conversation, inbox: inbox, items: items, head: head, settings: settings}, nil
}
func (b *canonicalBoundary) hasUsersOrReset() bool {
	for _, raw := range b.items {
		item := generationObject(raw)
		if item["mode"] == "steer" || item["mode"] == "followUp" || item["mode"] == "write" && generationObject(item["entry"])["head"] == "self" {
			return true
		}
	}
	return false
}
func (b *canonicalBoundary) apply(tx *Tx, final bool, now int64) (users []ID, first ID, reset bool, err error) {
	users = []ID{}
	for _, raw := range b.items {
		item := generationObject(raw)
		if item["mode"] == "write" && generationObject(item["entry"])["head"] == "self" {
			reset = true
		}
	}
	final = final || reset
	removed := map[int]bool{}
	for index, raw := range b.items {
		item := generationObject(raw)
		if item["mode"] != "write" {
			continue
		}
		removed[index] = true
		draft := generationObject(item["entry"])
		head := submissionEntryID(draft["head"])
		id := submissionEntryID(item["id"])
		if head != 0 && b.head != 0 && head < b.head {
			if err = tx.SettleSubmission(id, SubmissionSettlement{Status: "unanswered", Reason: "stale"}); err != nil {
				return
			}
			continue
		}
		var entry Entry
		entry, err = tx.AppendEntry(b.conversation, draft)
		if err != nil {
			return
		}
		if value, present := draft["head"]; present {
			b.head = head
			if value == "self" {
				b.head = entry.ID
			}
		}
		if err = tx.PlaceSubmission(id, entry.ID); err != nil {
			return
		}
		if first == 0 {
			first = entry.ID
		}
	}
	pickedSteer, pickedFollow := false, false
	for index, raw := range b.items {
		item := generationObject(raw)
		mode, _ := item["mode"].(string)
		if mode == "steer" {
			if pickedSteer && b.settings.SteeringMode != "all" {
				continue
			}
			pickedSteer = true
		} else if mode == "followUp" && final {
			if pickedFollow && b.settings.FollowUpMode != "all" {
				continue
			}
			pickedFollow = true
		} else {
			continue
		}
		draft := JSON{"kind": userEntryKind}
		if metadata := generationObject(item["entry"]); metadata != nil {
			draft["kind"] = metadata["kind"]
			if value, present := metadata["data"]; present {
				draft["data"] = value
			}
		}
		draft["model"] = []any{JSON{"role": "user", "content": item["content"], "timestamp": now}}
		var entry Entry
		entry, err = tx.AppendEntry(b.conversation, draft)
		if err != nil {
			return
		}
		id := submissionEntryID(item["id"])
		if err = tx.PlaceSubmission(id, entry.ID); err != nil {
			return
		}
		users = append(users, id)
		removed[index] = true
		if first == 0 {
			first = entry.ID
		}
	}
	keep := make([]any, 0, len(b.items))
	for index, item := range b.items {
		if !removed[index] {
			keep = append(keep, item)
		}
	}
	err = b.inbox.Set(JSON{"items": keep})
	return
}
func canonicalWithdrawInputs(tx *Tx, conversation ID, reason string) error {
	doc, err := builtin(tx, conversation, "pi.inbox")
	if err != nil {
		return err
	}
	value, err := doc.Get()
	if err != nil {
		return err
	}
	items, _ := value["items"].([]any)
	keep := []any{}
	for _, raw := range items {
		item := generationObject(raw)
		if item["mode"] == "write" {
			keep = append(keep, item)
			continue
		}
		if err := tx.SettleSubmission(submissionEntryID(item["id"]), SubmissionSettlement{Status: "unanswered", Reason: reason}); err != nil {
			return err
		}
	}
	return doc.Set(JSON{"items": keep})
}
func canonicalEndRun(tx *Tx, live JSON, task ID, answer ID, reason string, detail *string) error {
	run := generationObject(live["run"])
	if submissionEntryID(run["taskId"]) == task {
		inputs, _ := run["inputs"].([]any)
		if answer != 0 {
			for _, id := range inputs {
				if err := tx.SettleSubmission(submissionEntryID(id), SubmissionSettlement{Status: "done", Answer: answer}); err != nil {
					return err
				}
			}
		} else if run["afterRun"] == nil {
			settlement := SubmissionSettlement{Status: "unanswered", Reason: reason}
			if detail != nil && reason != "no_model" {
				settlement.Detail = &TaskValue{Present: true, Value: *detail}
			}
			for _, id := range inputs {
				if err := tx.SettleSubmission(submissionEntryID(id), settlement); err != nil {
					return err
				}
			}
			failure := JSON{"taskId": task, "inputs": inputs, "reason": reason}
			if detail != nil {
				failure["detail"] = *detail
			}
			if first, present := run["first"]; present {
				failure["first"] = first
			}
			live["runFailure"] = failure
		}
		delete(live, "run")
	}
	for _, key := range []string{"generation", "tools", "nestedTools"} {
		delete(live, key)
	}
	return nil
}
func canonicalStartRun(tx *Tx, conversation ID, live JSON, inputs []ID, first ID, keepFailure bool) (ID, error) {
	id, err := tx.CreateTask(GenerationTask(), JSON{}, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "conversation"}, SubmissionIDs: inputs})
	if err != nil {
		return 0, err
	}
	ids := make([]any, len(inputs))
	for index, id := range inputs {
		ids[index] = id
	}
	run := JSON{"taskId": id, "inputs": ids, "turns": 0}
	if first != 0 {
		run["first"] = first
	}
	live["run"] = run
	if !keepFailure {
		delete(live, "runFailure")
	}
	return id, nil
}
