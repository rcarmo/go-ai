package durable

// CreateSubmission creates a raw lifecycle record without Harness admission.
// Applications implementing their own admission may place/settle it in this
// transaction. Fresh IDs are reserved exactly as for other Session records.
func (t *Tx) CreateSubmission(record SubmissionRecord) (ID, error) {
	if err := t.enter(); err != nil {
		return 0, err
	}
	defer t.leave()
	if record.ID != 0 {
		return 0, reject("submission creation assigns identity")
	}
	sub, err := nativeSubmission(record, t.limits)
	if err != nil {
		return 0, err
	}
	state, err := t.current()
	if err != nil {
		return 0, err
	}
	if record.Entry != 0 && !entryVisible(state, record.Conversation, record.Entry) {
		return 0, reject("submission entry is not visible")
	}
	if record.Answer != 0 && !entryVisible(state, record.Conversation, record.Answer) {
		return 0, reject("submission answer is not visible")
	}
	id, err := t.store.mintID(t.ctx, true)
	if err != nil {
		return 0, err
	}
	t.state.HighWater = uint64(id)
	sub.ID = id
	if err := t.stage(Write{Op: "put-submission", Submission: &sub}); err != nil {
		return 0, err
	}
	return id, nil
}
func nativeSubmission(record SubmissionRecord, l Limits) (Submission, error) {
	value := JSON{}
	if record.Value != nil {
		owned, err := copyObject(record.Value, l)
		if err != nil {
			return Submission{}, err
		}
		value = owned
	}
	sub := Submission{ID: record.ID, Conversation: record.Conversation, RequestID: record.RequestID, Value: value, Trace: copyTelemetryCarrier(record.Trace)}
	switch record.Type {
	case "input":
		sub.Type = "follow-up"
	case "write", "continue":
		sub.Type = record.Type
	default:
		return Submission{}, reject("submission type")
	}
	if record.Type == "continue" {
		if record.Entry != 0 {
			return Submission{}, reject("continue submission has no entry")
		}
		// These fields belong to the typed lifecycle, not compatibility data.
		// Reject rather than silently erase a caller-supplied placement/answer.
		for _, field := range []string{"placedEntry", "entry", "answerEntry", "answer"} {
			if _, present := value[field]; present {
				return Submission{}, reject("continue submission lifecycle field in value")
			}
		}
		if _, present := value["detail"]; present && record.Status != "unanswered" {
			return Submission{}, reject("detail requires unanswered submission")
		}
	}
	switch record.Status {
	case "queued":
		sub.Status = "pending"
		if record.Type == "continue" || record.Entry != 0 || record.Answer != 0 || record.Reason != "" {
			return Submission{}, reject("queued submission fields")
		}
	case "placed":
		sub.Status = "pending"
		if record.Type == "write" || record.Type == "input" && record.Entry == 0 || record.Answer != 0 || record.Reason != "" {
			return Submission{}, reject("placed submission fields")
		}
	case "done":
		sub.Status = "done"
		if record.Type != "continue" && record.Entry == 0 || record.Type != "write" && record.Answer == 0 || record.Type == "write" && record.Answer != 0 || record.Reason != "" {
			return Submission{}, reject("done submission fields")
		}
	case "unanswered":
		sub.Status = "failed"
		if record.Answer != 0 {
			return Submission{}, reject("unanswered submission fields")
		}
		// The write union already requires a present reason for this status; an
		// explicitly empty one is stored and read back empty, as in the original.
		value["errorCode"] = record.Reason
	default:
		return Submission{}, reject("submission status")
	}
	if record.Detail != nil {
		if record.Status != "unanswered" {
			return Submission{}, reject("detail requires unanswered submission")
		}
		detail, err := copyTaskValue(record.Detail, l)
		if err != nil {
			return Submission{}, err
		}
		value["detail"] = detail.Value
	} else {
		delete(value, "detail")
	}
	delete(value, "placedEntry")
	delete(value, "answerEntry")
	if record.Entry != 0 {
		value["placedEntry"] = record.Entry
	}
	if record.Answer != 0 {
		value["answerEntry"] = record.Answer
	}
	return sub, nil
}

// Submission returns the transaction's latest staged lifecycle record.
func (t *Tx) Submission(id ID) (SubmissionRecord, bool, error) {
	if err := t.enter(); err != nil {
		return SubmissionRecord{}, false, err
	}
	defer t.leave()
	state, err := t.current()
	if err != nil {
		return SubmissionRecord{}, false, err
	}
	sub, ok := state.Submissions[id]
	if !ok {
		return SubmissionRecord{}, false, nil
	}
	sub.Value, err = copyObject(sub.Value, t.limits)
	if err != nil {
		return SubmissionRecord{}, false, err
	}
	return publicSubmission(sub), true, nil
}

// PlaceSubmission places a queued input, or completes a queued passive write.
// A continue is already placed and can never acquire an entry.
func (t *Tx) PlaceSubmission(id, entry ID) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	state, err := t.current()
	if err != nil {
		return err
	}
	sub, ok := state.Submissions[id]
	if !ok {
		return reject("submission does not exist")
	}
	if sub.Type == "continue" {
		return reject("continue submission has no entry")
	}
	if terminalStatus(sub.Status) {
		return nil
	}
	if submissionEntryID(sub.Value["placedEntry"]) != 0 {
		return reject("submission is not queued")
	}
	if entry == 0 || !entryVisible(state, sub.Conversation, entry) {
		return reject("submission entry is not visible")
	}
	sub.Value, err = copyObject(sub.Value, t.limits)
	if err != nil {
		return err
	}
	sub.Value["placedEntry"] = entry
	if sub.Type == "write" {
		sub.Status = "done"
	}
	return t.stage(Write{Op: "put-submission", Submission: &sub})
}

// SettleSubmission preserves the first terminal receipt and rejects an answer
// for an unplaced input or a write. Entryless continues may answer directly.
// It resolves earlier writes in this batch.
func (t *Tx) SettleSubmission(id ID, settlement SubmissionRecord) error {
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	state, err := t.current()
	if err != nil {
		return err
	}
	sub, ok := state.Submissions[id]
	if !ok {
		return reject("submission does not exist")
	}
	if terminalStatus(sub.Status) {
		return nil
	}
	if sub.Type == "continue" && settlement.Entry != 0 {
		return reject("continue submission has no entry")
	}
	sub.Value, err = copyObject(sub.Value, t.limits)
	if err != nil {
		return err
	}
	switch settlement.Status {
	case "done":
		if sub.Type == "write" || sub.Type != "continue" && submissionEntryID(sub.Value["placedEntry"]) == 0 {
			return reject("submission is not a placed input or continue")
		}
		if sub.Type == "continue" && (settlement.Reason != "" || settlement.Detail != nil) {
			return reject("done submission fields")
		}
		if settlement.Answer == 0 || !entryVisible(state, sub.Conversation, settlement.Answer) {
			return reject("submission answer is not visible")
		}
		sub.Status = "done"
		sub.Value["answerEntry"] = settlement.Answer
	case "unanswered":
		if settlement.Reason == "" || settlement.Answer != 0 {
			return reject("invalid unanswered submission settlement")
		}
		if settlement.Detail != nil {
			detail, err := copyTaskValue(settlement.Detail, t.limits)
			if err != nil {
				return err
			}
			sub.Value["detail"] = detail.Value
		}
		sub.Status = "failed"
		sub.Value["errorCode"] = settlement.Reason
	default:
		return reject("submission settlement status")
	}
	return t.stage(Write{Op: "put-submission", Submission: &sub})
}
