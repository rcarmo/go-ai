package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strconv"
	"time"
)

type Input struct {
	Content string
	Blocks  []goai.ContentBlock
	// Entry is a passive draft for Type=write; ID/placement are assigned on the
	// boundary. Nil retains the backwards-compatible user-text write.
	Entry     *Entry
	RequestID string
	Type      string
	// WhenBusy selects steer/follow-up for inputs, or reject to refuse a
	// new input while a generation is live. Deduplication precedes rejection.
	WhenBusy string
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
	Phase                 string               `json:"phase"`
	Submission            ID                   `json:"submission"`
	Input                 string               `json:"input"`
	InputBlocks           []goai.ContentBlock  `json:"inputBlocks,omitempty"`
	Agent                 agentState           `json:"agent"`
	Model                 *goai.Model          `json:"model,omitempty"`
	Messages              []messageReceipt     `json:"messages"`
	Attempt               uint64               `json:"attempt"`
	Partial               *MessageReceipt      `json:"partial,omitempty"`
	Offered               []toolOffer          `json:"offered,omitempty"`
	Children              []ID                 `json:"children,omitempty"`
	Sequential            bool                 `json:"sequential,omitempty"`
	ToolExecution         string               `json:"toolExecution,omitempty"`
	Abort                 bool                 `json:"abort,omitempty"`
	Round                 uint64               `json:"round,omitempty"`
	Steered               []ID                 `json:"steered,omitempty"`
	Reset                 bool                 `json:"reset,omitempty"`
	RetryCount            int                  `json:"retryCount,omitempty"`
	RetryUntil            int64                `json:"retryUntil,omitempty"`
	Compaction            ID                   `json:"compaction,omitempty"`
	ResumeAfterCompaction bool                 `json:"resumeAfterCompaction,omitempty"`
	Deferred              *goai.DeferredHandle `json:"deferred,omitempty"`
	PollAt                int64                `json:"pollAt,omitempty"`
}

func (c *ConversationHandle) Submit(ctx context.Context, input Input) (*SubmissionHandle, error) {
	return c.submitBound(ctx, input, nil)
}
func (c *ConversationHandle) submitBound(ctx context.Context, input Input, binding *TaskRuntime) (*SubmissionHandle, error) {
	if binding != nil {
		if err := binding.check(); err != nil {
			return nil, err
		}
	}
	return c.submitAdmission(ctx, input, binding)
}

// submitAdmission performs the authoritative lifetime/dedup action. Kept
// separate from the early fast check so queued admission uses one line fence.
func (c *ConversationHandle) submitAdmission(ctx context.Context, input Input, binding *TaskRuntime) (*SubmissionHandle, error) {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return nil, ErrClosed
	}
	if input.WhenBusy != "" && input.WhenBusy != "reject" && input.WhenBusy != "steer" && input.WhenBusy != "follow-up" {
		return nil, reject("invalid busy mode")
	}
	if input.Type == "" {
		input.Type = "follow-up"
		if input.WhenBusy == "steer" {
			input.Type = "steer"
		}
	}
	if input.Entry != nil && input.Type != "write" {
		return nil, reject("entry requires write submission")
	}
	if input.Entry != nil && (input.Content != "" || len(input.Blocks) > 0) {
		return nil, reject("passive entry and content conflict")
	}
	if input.Type != "follow-up" && input.Type != "steer" && input.Type != "write" {
		return nil, ErrUnsupported
	}
	if len(input.RequestID) > h.session.limits.MaxRequestIDBytes {
		return nil, reject("request ID limit")
	}
	var state Snapshot
	var dedupID ID
	e := h.session.readTasks(ctx, func(current Snapshot) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if binding != nil {
			if err := binding.check(); err != nil {
				return err
			}
			if taskAborted(current.Tasks[binding.taskID]) && !binding.abortMode {
				return reject("bound submit after abort")
			}
		}
		state = current
		if _, ok := current.Conversations[c.id]; !ok {
			return reject("unknown conversation")
		}
		for _, sub := range current.Submissions {
			if input.RequestID != "" && sub.Conversation == c.id && sub.RequestID == input.RequestID {
				if sub.Type != input.Type {
					return reject("request ID cross-type conflict")
				}
				dedupID = sub.ID
				if !terminalStatus(sub.Status) {
					h.scheduler.enable()
				}
				break
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	if _, ok := state.Conversations[c.id]; !ok {
		return nil, reject("unknown conversation")
	}
	if dedupID != 0 {
		// Lookup, bound lifetime/mark check and any pending wake all belonged
		// to the same line admission. Only diagnostic occupancy changes here.
		if !terminalStatus(state.Submissions[dedupID].Status) {
			h.workers[c.id] = true
		}
		return &SubmissionHandle{h, dedupID}, nil
	}
	if _, ok := agentDocument(state, c.id); !ok {
		return nil, reject("agent not configured")
	}
	return c.submitNewAdmission(ctx, input, binding)
}

// submitNewAdmission is the fresh-write admission AFTER the initial committed
// lookup. The caller owns h.mu; lifetime/Close is rechecked inside the write.
func (c *ConversationHandle) submitNewAdmission(ctx context.Context, input Input, binding *TaskRuntime) (*SubmissionHandle, error) {
	h := c.h
	var subID ID
	_, e := h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if binding != nil {
			if err := binding.check(); err != nil {
				return err
			}
			if taskAborted(tx.state.Tasks[binding.taskID]) && !binding.abortMode {
				return reject("bound submit after abort")
			}
			tx.byTask = binding.taskID
			tx.taskConversation = c.id
		}
		receipt, err := inputReceipt(input.Content, input.Blocks, tx.limits)
		if err != nil {
			return err
		}
		busy := conversationGenerationBusy(tx.state, c.id)
		if input.WhenBusy == "reject" && input.Type != "write" && busy {
			return ErrConversationBusy
		}
		subID, err = tx.MintID()
		if err != nil {
			return err
		}
		value := JSON{"content": input.Content}
		if len(input.Blocks) > 0 {
			wire, err := dtoObject(receipt, tx.limits)
			if err != nil {
				return err
			}
			value["inputMessage"] = map[string]any(wire)
		}
		status := "pending"
		if input.Type == "write" {
			if !busy {
				status = "done"
			}
			if input.Entry != nil {
				entry, err := copyEntry(*input.Entry, tx.limits)
				if err != nil {
					return err
				}
				entry.ID, entry.Conversation, entry.Seq, entry.Position = 0, 0, 0, 0
				wire, err := dtoObject(entry, tx.limits)
				if err != nil {
					return err
				}
				value["entry"] = map[string]any(wire)
			}
		}
		if err = tx.PutSubmission(Submission{ID: subID, Conversation: c.id, RequestID: input.RequestID, Type: input.Type, Status: status, Value: value}); err != nil {
			return err
		}
		if input.Type == "write" {
			if busy {
				inbox, err := builtin(tx, c.id, "pi.inbox")
				if err != nil {
					return err
				}
				return inbox.Update(func(v JSON) error {
					items, ok := v["items"].([]any)
					if !ok {
						return reject("inbox shape")
					}
					v["items"] = append(items, JSON{"id": subID, "mode": "write"})
					return nil
				})
			}
			if input.Entry != nil {
				stale, err := passiveHeadStale(tx.state, c.id, value, tx.limits)
				if err != nil {
					return err
				}
				if stale {
					value["errorCode"] = "stale"
					return tx.PutSubmission(Submission{ID: subID, Conversation: c.id, RequestID: input.RequestID, Type: "write", Status: "failed", Value: value})
				}
				return appendPassiveWrite(tx, c.id, value)
			}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			obj, err := dtoObject(receipt, h.session.limits)
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: c.id, Kind: "message", Value: obj})
		}
		taskID, err := tx.MintID()
		if err != nil {
			return err
		}
		checkpoint, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: subID, Input: input.Content, InputBlocks: input.Blocks}, h.session.limits)
		if err != nil {
			return err
		}
		// Private submission adapter, not public raw execution authority.
		task := Task{ID: taskID, Conversation: c.id, Kind: "pi.generation", Status: "pending", Checkpoint: checkpoint}
		if err = tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
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
			mode := "followUp"
			if input.Type == "steer" {
				mode = "steer"
			}
			v["items"] = append(items, JSON{"id": subID, "mode": mode, "input": value})
			return nil
		})
	})
	if e != nil {
		return nil, e
	}
	if input.Type == "follow-up" || input.Type == "steer" {
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
		var result Settlement
		terminal := false
		e := s.h.session.readTasks(ctx, func(state Snapshot) error {
			sub, ok := state.Submissions[s.id]
			if !ok {
				return reject("unknown submission")
			}
			terminal = terminalStatus(sub.Status)
			if !terminal {
				return nil
			}
			value, err := copyObject(sub.Value, s.h.session.limits)
			if err != nil {
				return err
			}
			sub.Value = value
			result.Submission = sub
			for _, id := range ids(state.Tasks) {
				task := state.Tasks[id]
				if task.Kind != "pi.generation" {
					continue
				}
				var cp generationCheckpoint
				if fromObject(task.Checkpoint, &cp, s.h.session.limits) == nil && generationIncludesSubmission(cp, s.id) && cp.Phase != "queued" {
					result.Task, err = copyTask(task, s.h.session.limits)
					if err != nil {
						return err
					}
					break
				}
			}
			if value, ok := sub.Value["message"].(map[string]any); ok {
				var m messageReceipt
				if err := fromObject(JSON(value), &m, s.h.session.limits); err != nil {
					return err
				}
				result.Message = &m
			}
			return nil
		})
		if e != nil {
			return Settlement{}, e
		}
		if terminal {
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
		if h.closing.Load() {
			return ErrClosed
		}
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
				if len(h.scheduler.ownedLive(tx.state, t.ID)) > 0 {
					return reject("queued submission owns live work; abort its conversation")
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
		if tx.taskTerminals == nil {
			tx.taskTerminals = map[ID]bool{}
		}
		tx.taskTerminals[task.ID] = true
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

func conversationGenerationBusy(state Snapshot, id ID) bool {
	for _, task := range state.Tasks {
		if task.Kind == "pi.generation" && task.Conversation == id && !terminalStatus(task.Status) {
			return true
		}
	}
	return false
}

func inputReceipt(text string, blocks []goai.ContentBlock, limits Limits) (MessageReceipt, error) {
	if len(blocks) == 0 {
		return userReceipt(text), nil
	}
	content := []goai.ContentBlock{}
	if text != "" {
		content = append(content, goai.ContentBlock{Type: "text", Text: text})
	}
	content = append(content, blocks...)
	return contributionReceipt(goai.Message{Role: goai.RoleUser, Content: content}, limits)
}
