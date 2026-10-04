package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strconv"
	"time"
)

type Input struct {
	Content   string
	RequestID string
	Type      string
}
type SubmissionHandle struct {
	h  *Harness
	id ID
}
type Settlement struct {
	Submission Submission
	Task       Task
	Message    *messageReceipt
}
type generationCheckpoint struct {
	Phase      string           `json:"phase"`
	Submission ID               `json:"submission"`
	Input      string           `json:"input"`
	Agent      agentState       `json:"agent"`
	Model      *goai.Model      `json:"model,omitempty"`
	Messages   []messageReceipt `json:"messages"`
	Attempt    uint64           `json:"attempt"`
	Partial    *MessageReceipt  `json:"partial,omitempty"`
	Offered    []toolOffer      `json:"offered,omitempty"`
	Children   []ID             `json:"children,omitempty"`
	Abort      bool             `json:"abort,omitempty"`
	Round      uint64           `json:"round,omitempty"`
}

func (c *ConversationHandle) Submit(ctx context.Context, input Input) (*SubmissionHandle, error) {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return nil, ErrClosed
	}
	if input.Type == "" {
		input.Type = "follow-up"
	}
	if input.Type != "follow-up" && input.Type != "write" {
		return nil, ErrUnsupported
	}
	if len(input.RequestID) > h.session.limits.MaxRequestIDBytes {
		return nil, reject("request ID limit")
	}
	state, e := h.session.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := state.Conversations[c.id]; !ok {
		return nil, reject("unknown conversation")
	}
	for _, sub := range state.Submissions {
		if input.RequestID != "" && sub.Conversation == c.id && sub.RequestID == input.RequestID {
			if sub.Type != input.Type {
				return nil, reject("request ID cross-type conflict")
			}
			if !terminalStatus(sub.Status) {
				h.scheduleLocked(c.id)
			}
			return &SubmissionHandle{h, sub.ID}, nil
		}
	}
	if _, ok := agentDocument(state, c.id); !ok {
		return nil, reject("agent not configured")
	}
	var subID ID
	_, e = h.session.Commit(ctx, func(tx *Tx) error {
		var err error
		subID, err = tx.MintID()
		if err != nil {
			return err
		}
		value := JSON{"content": input.Content}
		status := "pending"
		if input.Type == "write" {
			status = "done"
		}
		if err = tx.PutSubmission(Submission{ID: subID, Conversation: c.id, RequestID: input.RequestID, Type: input.Type, Status: status, Value: value}); err != nil {
			return err
		}
		if input.Type == "write" {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			obj, err := dtoObject(userReceipt(input.Content), h.session.limits)
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: c.id, Kind: "message", Value: obj})
		}
		taskID, err := tx.MintID()
		if err != nil {
			return err
		}
		checkpoint, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: subID, Input: input.Content}, h.session.limits)
		if err != nil {
			return err
		}
		if err = tx.PutTask(Task{ID: taskID, Conversation: c.id, Kind: "pi.generation", Status: "pending", Checkpoint: checkpoint}); err != nil {
			return err
		}
		inbox, err := builtin(tx, c.id, "pi.inbox")
		if err != nil {
			return err
		}
		return inbox.Update(func(v JSON) error {
			items, ok := v["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			v["items"] = append(items, JSON{"id": subID, "mode": "followUp", "input": JSON{"content": input.Content}})
			return nil
		})
	})
	if e != nil {
		return nil, e
	}
	if input.Type == "follow-up" {
		h.scheduleLocked(c.id)
	}
	return &SubmissionHandle{h, subID}, nil
}
func (h *Harness) Submission(ctx context.Context, id ID) (*SubmissionHandle, error) {
	s, e := h.session.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := s.Submissions[id]; !ok {
		return nil, reject("unknown submission")
	}
	return &SubmissionHandle{h, id}, nil
}
func (s *SubmissionHandle) ID() ID { return s.id }
func (s *SubmissionHandle) Wait(ctx context.Context) (Settlement, error) {
	if ctx == nil {
		return Settlement{}, reject("nil context")
	}
	if e := s.h.Resume(ctx); e != nil {
		return Settlement{}, e
	}
	for {
		signal := s.h.signal()
		state, e := s.h.Snapshot(ctx)
		if e != nil {
			return Settlement{}, e
		}
		sub, ok := state.Submissions[s.id]
		if !ok {
			return Settlement{}, reject("unknown submission")
		}
		if terminalStatus(sub.Status) {
			result := Settlement{Submission: sub}
			for _, task := range state.Tasks {
				if task.Kind != "pi.generation" {
					continue
				}
				var cp generationCheckpoint
				if fromObject(task.Checkpoint, &cp, s.h.session.limits) == nil && cp.Submission == s.id {
					result.Task = task
					break
				}
			}
			if value, ok := sub.Value["message"].(map[string]any); ok {
				var m messageReceipt
				if e = fromObject(JSON(value), &m, s.h.session.limits); e != nil {
					return Settlement{}, e
				}
				result.Message = &m
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			return Settlement{}, ctx.Err()
		case <-signal:
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// Withdraw only settles a queued follow-up; already prepared/running input needs
// Abort. Withdrawal is durable and never silently cancels a shared invocation.
func (s *SubmissionHandle) Withdraw(ctx context.Context) error {
	h := s.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return ErrClosed
	}
	_, e := h.session.Commit(ctx, func(tx *Tx) error {
		sub, ok := tx.state.Submissions[s.id]
		if !ok {
			return reject("unknown submission")
		}
		if terminalStatus(sub.Status) {
			return nil
		}
		var task Task
		found := false
		for _, t := range tx.state.Tasks {
			if t.Kind != "pi.generation" {
				continue
			}
			var cp generationCheckpoint
			if e := fromObject(t.Checkpoint, &cp, h.session.limits); e != nil {
				return e
			}
			if cp.Submission == s.id {
				if cp.Phase != "queued" {
					return reject("submission already placed")
				}
				task = t
				cp.Phase = "terminal"
				value, e := dtoObject(cp, h.session.limits)
				if e != nil {
					return e
				}
				task.Checkpoint = value
				task.Status = "aborted"
				found = true
				break
			}
		}
		if !found {
			return reject("queued task unavailable")
		}
		if e := tx.PutTask(task); e != nil {
			return e
		}
		sub.Status = "aborted"
		sub.Value = JSON{"errorCode": "withdrawn"}
		if e := tx.PutSubmission(sub); e != nil {
			return e
		}
		inbox, e := builtin(tx, sub.Conversation, "pi.inbox")
		if e != nil {
			return e
		}
		return inbox.Update(func(v JSON) error {
			items, ok := v["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			keep := []any{}
			for _, item := range items {
				obj, ok := item.(map[string]any)
				if !ok {
					return reject("inbox item")
				}
				if fmt.Sprint(obj["id"]) != strconv.FormatUint(uint64(s.id), 10) {
					keep = append(keep, item)
				}
			}
			v["items"] = keep
			return nil
		})
	})
	return e
}
