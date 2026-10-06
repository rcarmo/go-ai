package durable

import (
	"context"
	"fmt"
	"strconv"
)

// SubmissionRecord exposes the reference lifecycle while Snapshot retains
// native storage envelopes. Value is detached compatibility data.
type SubmissionRecord struct {
	ID           ID
	Conversation ID
	RequestID    string
	Type         string
	Status       string
	Entry        ID
	Answer       ID
	Reason       string
	Detail       *TaskValue // Optional arbitrary JSON; Present distinguishes null.
	Value        JSON
}

// Status reads committed lifecycle without enabling scheduling.
func (s *SubmissionHandle) Status(ctx context.Context) (SubmissionRecord, error) {
	return s.status(ctx, nil)
}
func (s *SubmissionHandle) status(ctx context.Context, binding *TaskRuntime) (SubmissionRecord, error) {
	if binding != nil {
		if err := binding.check(); err != nil {
			return SubmissionRecord{}, err
		}
		if ctx == nil {
			return SubmissionRecord{}, reject("nil context")
		}
		combined, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(binding.context, cancel)
		defer func() { stop(); cancel() }()
		ctx = combined
	}
	if s.h.closing.Load() {
		return SubmissionRecord{}, ErrClosed
	}
	var result SubmissionRecord
	err := s.h.session.readTasks(ctx, func(state Snapshot) error {
		if binding != nil {
			if err := binding.check(); err != nil {
				return err
			}
		}
		if s.h.closing.Load() {
			return ErrClosed
		}
		sub, ok := state.Submissions[s.id]
		if !ok {
			return reject("unknown submission")
		}
		value, err := copyObject(sub.Value, s.h.session.limits)
		if err != nil {
			return err
		}
		sub.Value = value
		result = publicSubmission(sub)
		return nil
	})
	if err == nil && binding != nil {
		err = binding.check()
	}
	return result, err
}

// Abort withdraws queued input or writes; it does not cancel already placed
// work, and reports its current disposition instead of altering a shared run.
func (s *SubmissionHandle) Abort(ctx context.Context) (string, error) {
	return s.h.abortSubmission(ctx, s.id, nil, nil)
}

// AbortSubmission optionally scopes lookup to a conversation. At most one
// scope may be supplied; absent/wrong-scope IDs return not_found.
func (h *Harness) AbortSubmission(ctx context.Context, id ID, conversation ...ID) (string, error) {
	if len(conversation) > 1 {
		return "", reject("submission scope count")
	}
	var scope *ID
	if len(conversation) > 0 {
		scope = &conversation[0]
	}
	return h.abortSubmission(ctx, id, scope, nil)
}
func (h *Harness) abortSubmission(ctx context.Context, id ID, scope *ID, binding *TaskRuntime) (string, error) {
	if binding != nil {
		if err := binding.check(); err != nil {
			return "", err
		}
		if ctx == nil {
			return "", reject("nil context")
		}
		combined, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(binding.context, cancel)
		defer func() { stop(); cancel() }()
		ctx = combined
	}
	if h.closing.Load() {
		return "", ErrClosed
	}
	result := "not_found"
	_, err := h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if binding != nil {
			if err := binding.check(); err != nil {
				return err
			}
			if taskAborted(tx.state.Tasks[binding.taskID]) && !binding.abortMode {
				return ErrSealed
			}
		}
		sub, ok := tx.state.Submissions[id]
		if !ok || scope != nil && sub.Conversation != *scope {
			return nil
		}
		if terminalStatus(sub.Status) {
			result = "settled"
			return nil
		}
		if submissionEntryID(sub.Value["placedEntry"]) != 0 {
			result = "already_placed"
			return nil
		}
		var queued *Task
		for _, taskID := range ids(tx.state.Tasks) {
			task := tx.state.Tasks[taskID]
			if task.Kind != "pi.generation" {
				continue
			}
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
				return err
			}
			if !generationIncludesSubmission(cp, id) {
				continue
			}
			if cp.Phase != "queued" || cp.InputEntry != 0 {
				// Terminal queued placeholders may name inputs adopted by a
				// successor; only a live/final owning run proves placement.
				if terminalStatus(task.Status) {
					continue
				}
				result = "already_placed"
				return nil
			}
			if len(h.scheduler.ownedLive(tx.state, task.ID)) > 0 {
				return reject("queued submission owns live work; abort its conversation")
			}
			owned, err := copyTask(task, tx.limits)
			if err != nil {
				return err
			}
			cp.Phase = "terminal"
			cp.Abort = true
			owned.Checkpoint, err = dtoObject(cp, tx.limits)
			if err != nil {
				return err
			}
			owned.Status = "aborted"
			queued = &owned
		}
		// Inbox membership is authoritative for queued writes and queued inputs.
		inbox, err := builtin(tx, sub.Conversation, "pi.inbox")
		if err != nil {
			return err
		}
		value, err := inbox.Get()
		if err != nil {
			return err
		}
		items, ok := value["items"].([]any)
		if !ok {
			return reject("inbox shape")
		}
		keep := make([]any, 0, len(items))
		for _, item := range items {
			object, ok := item.(map[string]any)
			if !ok {
				return reject("inbox item")
			}
			if fmt.Sprint(object["id"]) == strconv.FormatUint(uint64(id), 10) {
				continue
			}
			keep = append(keep, item)
		}
		// Raw queued submissions created by application commits need not
		// have an inbox item. Absence alone cannot prove placement.
		if queued != nil {
			if tx.taskTerminals == nil {
				tx.taskTerminals = map[ID]bool{}
			}
			tx.taskTerminals[queued.ID] = true
			if err := tx.stage(Write{Op: "put-task", Task: queued}); err != nil {
				return err
			}
		}
		sub.Status = "aborted"
		sub.Value = JSON{"errorCode": "aborted"}
		if err := tx.PutSubmission(sub); err != nil {
			return err
		}
		value["items"] = keep
		if err := inbox.Set(value); err != nil {
			return err
		}
		result = "aborted"
		return nil
	})
	return result, err
}

func (s *InvocationSubmission) Status(ctx context.Context) (SubmissionRecord, error) {
	return s.handle.status(ctx, s.runtime)
}
func (s *InvocationSubmission) Abort(ctx context.Context) (string, error) {
	return s.runtime.harness.abortSubmission(ctx, s.ID(), nil, s.runtime)
}

func (h *Harness) addSubmissionWaiter(id ID, wake chan struct{}) error {
	total := 0
	for _, waiters := range h.submissionWaiters {
		total += len(waiters)
	}
	if total >= h.session.limits.MaxPage {
		return reject("submission waiter limit")
	}
	if h.submissionWaiters == nil {
		h.submissionWaiters = map[ID]map[chan struct{}]bool{}
	}
	if h.submissionWaiters[id] == nil {
		h.submissionWaiters[id] = map[chan struct{}]bool{}
	}
	h.submissionWaiters[id][wake] = true
	return nil
}
func (h *Harness) removeSubmissionWaiter(id ID, wake chan struct{}) {
	delete(h.submissionWaiters[id], wake)
	if len(h.submissionWaiters[id]) == 0 {
		delete(h.submissionWaiters, id)
	}
}
func (h *Harness) notifySubmissionWaiters(tx *Tx) {
	for _, write := range tx.writes {
		if write.Submission == nil {
			continue
		}
		for wake := range h.submissionWaiters[write.Submission.ID] {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
}

func (h *Harness) wakeAllSubmissionWaiters() {
	for _, waiters := range h.submissionWaiters {
		for wake := range waiters {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
}

func submissionEntryID(value any) ID {
	n, ok := exactNumber(value)
	if !ok || n == nil || !n.IsInt() || n.Sign() <= 0 || !n.Num().IsUint64() || n.Num().Uint64() > MaxID {
		return 0
	}
	return ID(n.Num().Uint64())
}
func publicSubmission(sub Submission) SubmissionRecord {
	record := SubmissionRecord{ID: sub.ID, Conversation: sub.Conversation, RequestID: sub.RequestID, Type: sub.Type, Value: sub.Value, Entry: submissionEntryID(sub.Value["placedEntry"]), Answer: submissionEntryID(sub.Value["answerEntry"])}
	if sub.Type != "write" {
		record.Type = "input"
	}
	switch sub.Status {
	case "pending", "running":
		record.Status = "queued"
		if record.Entry != 0 {
			record.Status = "placed"
		}
	case "done":
		record.Status = "done"
	default:
		record.Status = "unanswered"
		record.Answer = 0 // legacy envelopes may retain a receipt, never an answer
		record.Reason, _ = sub.Value["errorCode"].(string)
		if record.Reason == "" {
			record.Reason = sub.Status
		}
		if detail, found := sub.Value["detail"]; found {
			record.Detail = &TaskValue{Present: true, Value: detail}
		}
	}
	return record
}
func markSubmissionEntry(tx *Tx, id, entry ID) error {
	sub, ok := tx.state.Submissions[id]
	if !ok {
		return reject("submission unavailable")
	}
	value, err := copyObject(sub.Value, tx.limits)
	if err != nil {
		return err
	}
	value["placedEntry"] = entry
	sub.Value = value
	return tx.PutSubmission(sub)
}
func retainSubmissionPlacement(before, after JSON, answer ID) {
	if entry := submissionEntryID(before["placedEntry"]); entry != 0 {
		after["placedEntry"] = entry
	}
	if answer != 0 {
		after["answerEntry"] = answer
	}
}
