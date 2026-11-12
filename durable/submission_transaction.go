package durable

import (
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

// CreateSubmission creates a raw lifecycle record without Harness admission.
// Applications implementing their own admission may place/settle it in this
// transaction. Fresh IDs are reserved exactly as for other Session records.
func (t *Tx) CreateSubmission(create SubmissionCreate) (SubmissionRecord, error) {
	if err := t.enterKeys(); err != nil {
		return SubmissionRecord{}, err
	}
	defer t.leave()
	t.hasTableWrite = true
	if err := t.touchKeys(conversationKey(create.Conversation)); err != nil {
		return SubmissionRecord{}, err
	}
	state, err := t.current()
	if err != nil {
		return SubmissionRecord{}, err
	}
	if _, found := state.Conversations[create.Conversation]; !found {
		return SubmissionRecord{}, fmt.Errorf("Conversation %d does not exist", create.Conversation)
	}
	id, err := t.store.mintID(t.ctx, true)
	if err != nil {
		return SubmissionRecord{}, err
	}
	t.state.HighWater = uint64(id)
	record := SubmissionRecord{ID: id, Conversation: create.Conversation, RequestID: create.RequestID, RequestIDPresent: create.RequestIDPresent, Type: create.Type, Status: create.Status, Entry: create.Entry, Answer: create.Answer, Reason: create.Reason, Detail: create.Detail, Trace: create.Trace}
	native, err := nativeSubmission(record, t.limits)
	if err != nil {
		return SubmissionRecord{}, err
	}
	owned, err := canonicalStoredSubmission(native, t.limits)
	if err != nil {
		return SubmissionRecord{}, err
	}
	t.setSubmission(owned)
	return canonicalStoredSubmission(native, t.limits)
}
func nativeSubmission(record SubmissionRecord, l Limits) (Submission, error) {
	if err := record.validateWire(); err != nil {
		return Submission{}, err
	}
	value := JSON{}
	sub := Submission{ID: record.ID, Conversation: record.Conversation, RequestID: record.RequestID, RequestIDPresent: record.RequestIDPresent, Value: value, Trace: copyTelemetryCarrier(record.Trace)}
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

// SubmissionCreate omits the identity assigned by the Session, as the pinned
// SubmissionCreate union does. It carries no native input/compatibility payload.
type SubmissionCreate struct {
	Conversation     ID
	RequestID        string
	RequestIDPresent bool
	Type             string
	Status           string
	Entry            ID
	Answer           ID
	Reason           string
	Detail           *TaskValue
	Trace            *goai.TelemetryCarrier
}

// SubmissionSettlement cannot replace identity, type, request metadata or entry.
type SubmissionSettlement struct {
	Status string
	Answer ID
	Reason string
	Detail *TaskValue
}
type transactionSubmissionChange struct {
	id         ID
	placement  bool
	entry      ID
	settlement SubmissionSettlement
}

func (t *Tx) setSubmission(record SubmissionRecord) {
	if t.submissions == nil {
		t.submissions = map[ID]SubmissionRecord{}
	}
	if _, exists := t.submissions[record.ID]; !exists {
		t.submissionOrder = append(t.submissionOrder, record.ID)
	}
	t.submissions[record.ID] = record
}

// PlaceSubmission only queues a change. The current record is selected during
// assembly, after every callback operation has run.
func (t *Tx) PlaceSubmission(id, entry ID) error {
	if err := t.enterKeys(); err != nil {
		return err
	}
	defer t.leave()
	t.hasTableWrite = true
	t.submissionChanges = append(t.submissionChanges, transactionSubmissionChange{id: id, placement: true, entry: entry})
	return nil
}
func (t *Tx) SettleSubmission(id ID, change SubmissionSettlement) error {
	if err := t.enterKeys(); err != nil {
		return err
	}
	defer t.leave()
	t.hasTableWrite = true
	switch change.Status {
	case "done":
		if change.Answer == 0 || change.Reason != "" || change.Detail != nil {
			return reject("done submission settlement fields")
		}
	case "unanswered":
		if change.Answer != 0 {
			return reject("unanswered submission settlement fields")
		}
		if change.Detail != nil {
			owned, err := copyTaskValue(change.Detail, t.limits)
			if err != nil {
				return err
			}
			change.Detail = owned
		}
	default:
		return reject("submission settlement status")
	}
	t.submissionChanges = append(t.submissionChanges, transactionSubmissionChange{id: id, settlement: change})
	return nil
}

// applySubmissionChange is the pinned current-record transition: terminal
// records stay; placement requires queued; an answer requires placed.
func applySubmissionChange(current SubmissionRecord, change transactionSubmissionChange) (SubmissionRecord, bool, error) {
	if current.Status == "done" || current.Status == "unanswered" {
		return current, false, nil
	}
	if change.placement {
		if current.Status != "queued" {
			return SubmissionRecord{}, false, fmt.Errorf("Submission %d is not queued", current.ID)
		}
		current.Status = "done"
		if current.Type == "input" {
			current.Status = "placed"
		}
		current.Entry = change.entry
		return current, true, nil
	}
	if change.settlement.Status == "done" && current.Status != "placed" {
		return SubmissionRecord{}, false, fmt.Errorf("Submission %d is not a placed input or continue", current.ID)
	}
	current.Status = change.settlement.Status
	current.Answer = change.settlement.Answer
	current.Reason = change.settlement.Reason
	current.Detail = change.settlement.Detail
	return current, true, nil
}
func (t *Tx) finalizeSubmissionWrites() error {
	for _, change := range t.submissionChanges {
		current, found := t.submissions[change.id]
		if !found {
			if err := t.touchKeys(submissionKey(change.id)); err != nil {
				return err
			}
			var err error
			current, found, err = lookupSubmission(t.state, t.limits, change.id)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("Submission %d does not exist", change.id)
			}
		}
		next, changed, err := applySubmissionChange(current, change)
		if err != nil {
			return err
		}
		if changed {
			t.setSubmission(next)
		}
	}
	for _, id := range t.submissionOrder {
		native, err := nativeSubmission(t.submissions[id], t.limits)
		if err != nil {
			return err
		}
		// The physical envelope still contains the historical builtin payload. It
		// is not part of the public record and cannot be supplied by these APIs.
		if prior, found := t.state.Submissions[id]; found {
			value, err := copyObject(prior.Value, t.limits)
			if err != nil {
				return err
			}
			for _, key := range []string{"placedEntry", "answerEntry", "errorCode", "detail"} {
				delete(value, key)
			}
			for key, item := range native.Value {
				value[key] = item
			}
			native.Value = value
		}
		if err := t.stage(Write{Op: "put-submission", Submission: &native}); err != nil {
			return err
		}
	}
	return nil
}
