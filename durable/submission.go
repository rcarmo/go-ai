package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strconv"
)

type Input struct {
	Content string
	Blocks  []goai.ContentBlock
	// Entry is a passive draft for Type=write. For user input it accepts only
	// an application Kind and optional Data/HasData; the user message remains
	// its model contribution. ID/placement are assigned at admission.
	Entry     *Entry
	RequestID string
	// RequestIDPresent supplies an explicitly empty deduplication key. Nonempty
	// RequestID implies presence, preserving existing callers.
	RequestIDPresent bool
	// Type accepts "input", "write" and "continue". Empty, "follow-up" and
	// "steer" remain accepted as legacy input spellings.
	Type string
	// WhenBusy selects "steer" or "followUp" for inputs, or "reject" to
	// refuse a live generation. "follow-up" remains a legacy alias.
	// Deduplication precedes rejection.
	WhenBusy string
	// WhenIdle may be "reject" for input that must not start a new run.
	WhenIdle string
}

// ConversationBusy identifies the conversation that rejected busy admission.
type ConversationBusy struct{ ConversationID ID }

func (e *ConversationBusy) Error() string {
	return fmt.Sprintf("Conversation %d is busy", e.ConversationID)
}
func (e *ConversationBusy) Unwrap() error { return ErrConversationBusy }

// ConversationIdle identifies the conversation that rejected idle input.
type ConversationIdle struct{ ConversationID ID }

func (e *ConversationIdle) Error() string {
	return fmt.Sprintf("Conversation %d is idle", e.ConversationID)
}
func (e *ConversationIdle) Unwrap() error { return ErrConversationIdle }

type SubmissionHandle struct {
	h  *Harness
	id ID
}
type Settlement struct {
	Submission Submission       // Native compatibility envelope.
	Record     SubmissionRecord // Reference lifecycle projection.
	Task       Task
	Message    *messageReceipt
}
type generationCheckpoint struct {
	AfterRun              *RunSummary          `json:"afterRun,omitempty"`
	AfterRunTasks         []ID                 `json:"afterRunTasks,omitempty"`
	AfterFailedRun        string               `json:"afterFailedRun,omitempty"`
	FailureCut            ID                   `json:"failureCut,omitempty"`
	Phase                 string               `json:"phase"`
	Submission            ID                   `json:"submission"`
	InputMetadata         *storedEntry         `json:"inputMetadata,omitempty"` // private native envelope
	InputEntry            ID                   `json:"inputEntry,omitempty"`
	Input                 string               `json:"input"`
	InputBlocks           []goai.ContentBlock  `json:"inputBlocks,omitempty"`
	Agent                 agentState           `json:"agent"`
	Model                 *goai.Model          `json:"model,omitempty"`
	Messages              []messageReceipt     `json:"messages"`
	Attempt               uint64               `json:"attempt"`
	Partial               *MessageReceipt      `json:"partial,omitempty"`
	Offered               []toolOffer          `json:"offered,omitempty"`
	Children              []ID                 `json:"children,omitempty"`
	PendingTools          []toolCheckpoint     `json:"pendingTools,omitempty"`
	UnstartedCalls        []string             `json:"unstartedCalls,omitempty"`
	AssistantEntry        ID                   `json:"assistantEntry,omitempty"`
	Successor             ID                   `json:"successor,omitempty"`
	Cutoff                ID                   `json:"cutoff,omitempty"` // newest entry included by the charged request intent
	Sequential            bool                 `json:"sequential,omitempty"`
	ToolExecution         string               `json:"toolExecution,omitempty"`
	Abort                 bool                 `json:"abort,omitempty"`
	Round                 uint64               `json:"round,omitempty"`
	Steered               []ID                 `json:"steered,omitempty"`
	Reset                 bool                 `json:"reset,omitempty"`
	RetryCount            int                  `json:"retryCount,omitempty"`
	RetryUntil            int64                `json:"retryUntil,omitempty"`
	RetryMessage          string               `json:"retryMessage,omitempty"`
	Compaction            ID                   `json:"compaction,omitempty"`
	OverflowMessage       string               `json:"overflowMessage,omitempty"`
	ResumeAfterCompaction bool                 `json:"resumeAfterCompaction,omitempty"`
	Deferred              *goai.DeferredHandle `json:"deferred,omitempty"`
	PollAt                int64                `json:"pollAt,omitempty"`
}

func (c *ConversationHandle) Submit(ctx context.Context, input Input) (*SubmissionHandle, error) {
	return c.submitBound(ctx, input, nil)
}

// SubmitIn admits a draft in the caller's commit and returns its ID. Admission
// is stored or lost with that commit, and must precede its first table write.
func (c *ConversationHandle) SubmitIn(tx *Tx, input Input) (id ID, err error) {
	return c.submitIn(tx, input, nil)
}
func (c *ConversationHandle) submitIn(tx *Tx, input Input, binding *TaskRuntime) (id ID, err error) {
	if binding != nil {
		if err = binding.check(); err != nil {
			return 0, err
		}
	}
	if tx == nil || tx.session != c.h.session || tx.identity != tx {
		//lint:ignore ST1005 Exact pinned transaction-ownership diagnostic.
		return 0, fmt.Errorf("The transaction belongs to another Session")
	}
	if tx.hasTableWrite {
		return 0, &ReadAfterWrite{Method: "submit(tx, draft)"}
	}
	if err = tx.enter(); err != nil {
		return 0, err
	}
	tx.leave()
	c.h.scheduler.enable()
	ctx := tx.ctx
	trace := c.beginSubmitTrace(ctx, input)
	if trace != nil {
		ctx = context.WithValue(ctx, submitTelemetryContextKey{}, trace)
		defer func() {
			var handle *SubmissionHandle
			if err == nil {
				handle = &SubmissionHandle{c.h, id}
			}
			trace.finish(handle, err)
		}()
	}
	input, err = c.prepareSubmissionInput(input)
	if err != nil {
		return 0, err
	}
	id, _, err = c.admitSubmission(ctx, tx, input, binding)
	if err == nil && trace != nil {
		state, e := tx.current()
		if e != nil {
			return 0, e
		}
		if sub, ok := state.Submissions[id]; ok {
			trace.observe(publicSubmission(sub))
		}
	}
	return id, err
}

func (c *ConversationHandle) submitBound(ctx context.Context, input Input, binding *TaskRuntime) (handle *SubmissionHandle, err error) {
	trace := c.beginSubmitTrace(ctx, input)
	if trace != nil {
		ctx = context.WithValue(ctx, submitTelemetryContextKey{}, trace)
		ctx = context.WithValue(ctx, telemetryEventsContextKey{}, &trace.events)
		defer func() { trace.finish(handle, err) }()
	}
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
	input, err := c.prepareSubmissionInput(input)
	if err != nil {
		return nil, err
	}
	return c.submitOnLine(ctx, input, binding)
}

// submitOnLine owns one Session commit from request lookup through admission.
// The caller owns h.mu; lifetime and binding are checked inside that commit.
func (c *ConversationHandle) submitOnLine(ctx context.Context, input Input, binding *TaskRuntime) (*SubmissionHandle, error) {
	h := c.h
	var subID ID
	var dedup *SubmissionRecord
	_, e := h.session.Commit(ctx, func(tx *Tx) (any, error) {
		var err error
		subID, dedup, err = c.admitSubmission(ctx, tx, input, binding)
		return nil, err
	})
	if e != nil {
		return nil, e
	}
	if dedup != nil {
		if dedup.Status != "done" && dedup.Status != "unanswered" {
			h.scheduler.enable()
			h.workers[c.id] = true
		}
		return &SubmissionHandle{h, subID}, nil
	}
	if input.Type == "follow-up" || input.Type == "steer" || input.Type == "continue" {
		h.scheduleLocked(c.id)
	}
	return &SubmissionHandle{h, subID}, nil
}
func (c *ConversationHandle) prepareSubmissionInput(input Input) (Input, error) {
	h := c.h
	if input.WhenBusy != "" && input.WhenBusy != "reject" && input.WhenBusy != "steer" && input.WhenBusy != "follow-up" && input.WhenBusy != "followUp" {
		return input, reject("invalid busy mode")
	}
	if input.WhenIdle != "" && input.WhenIdle != "reject" {
		return input, reject("invalid idle mode")
	}
	if input.WhenIdle != "" && input.Type != "" && input.Type != "input" && input.Type != "follow-up" && input.Type != "steer" {
		return input, reject("idle mode requires input")
	}
	if input.Type == "" || input.Type == "input" {
		input.Type = "follow-up"
		if input.WhenBusy == "steer" {
			input.Type = "steer"
		}
	}
	if input.Entry != nil && input.Type != "write" && input.Type != "continue" {
		if err := validateInputMetadata(input.Entry); err != nil {
			return input, err
		}
	}
	if input.Type == "write" && input.Entry != nil && (input.Content != "" || len(input.Blocks) > 0) {
		return input, reject("passive entry and content conflict")
	}
	if input.Type != "follow-up" && input.Type != "steer" && input.Type != "write" && input.Type != "continue" {
		return input, ErrUnsupported
	}
	if len(input.RequestID) > h.session.limits.MaxRequestIDBytes {
		return input, reject("request ID limit")
	}
	return input, nil
}

// admitSubmission is shared by standalone and caller-owned commits. It neither
// opens a transaction nor acquires Harness.mu (the caller may own the Session line).
func (c *ConversationHandle) admitSubmission(ctx context.Context, tx *Tx, input Input, binding *TaskRuntime) (ID, *SubmissionRecord, error) {
	h := c.h
	if err := tx.touchKeys(ALL_KEYS); err != nil {
		return 0, nil, err
	}
	if h.closing.Load() {
		return 0, nil, ErrClosed
	}
	if binding != nil {
		if err := binding.check(); err != nil {
			return 0, nil, err
		}
		if taskAborted(tx.state.Tasks[binding.taskID]) && !binding.abortMode {
			return 0, nil, reject("bound submit after abort")
		}
		tx.byTask = binding.taskID
		tx.taskConversation = c.id
	}
	if _, found := tx.state.Conversations[c.id]; !found {
		return 0, nil, reject("unknown conversation")
	}
	kind := "input"
	if input.Type == "write" || input.Type == "continue" {
		kind = input.Type
	}
	if input.RequestID != "" || input.RequestIDPresent {
		record, found, err := tx.SubmissionByRequest(c.id, input.RequestID)
		if err != nil {
			return 0, nil, err
		}
		if found {
			if record.Type != kind {
				//lint:ignore ST1005 Exact pinned submissions.ts request discriminator error.
				return 0, nil, fmt.Errorf("Request %s already identifies a submission of type %s", input.RequestID, record.Type)
			}
			if trace := submitTraceFromContext(ctx); trace != nil {
				trace.observe(record)
			}
			return record.ID, &record, nil
		}
	}
	carrier := submissionTelemetryCarrier(submitTraceFromContext(ctx))
	create := func(status string, entry ID, reason string) (SubmissionRecord, error) {
		return tx.CreateSubmission(SubmissionCreate{Conversation: c.id, Type: kind, Status: status, Entry: entry, Reason: reason, RequestID: input.RequestID, RequestIDPresent: input.RequestIDPresent, Trace: carrier})
	}
	now := h.session.now()
	settings := h.resolvedSettings(RequestSettings{})
	var draft JSON
	var content any = input.Content
	if input.Blocks != nil {
		message, err := inputReceipt(input.Content, input.Blocks, tx.limits)
		if err != nil {
			return 0, nil, err
		}
		wire, err := dtoObject(message, tx.limits)
		if err != nil {
			return 0, nil, err
		}
		content = wire["content"]
	}
	if kind == "write" {
		if input.Entry != nil {
			var err error
			draft, err = inboxEntryDraft(*input.Entry, tx.limits)
			if err != nil {
				return 0, nil, err
			}
		} else {
			draft = JSON{"kind": userEntryKind, "model": []any{JSON{"role": "user", "content": content, "timestamp": now}}}
		}
	}
	inbox, err := builtin(tx, c.id, "pi.inbox")
	if err != nil {
		return 0, nil, err
	}
	inboxValue, err := inbox.Get()
	if err != nil {
		return 0, nil, err
	}
	items, _ := inboxValue["items"].([]any)
	hasReset := false
	for _, raw := range items {
		item := generationObject(raw)
		if item["mode"] == "write" && generationObject(item["entry"])["head"] == "self" {
			hasReset = true
		}
	}
	_, model := draft["model"]
	_, head := draft["head"]
	_, edits := draft["edits"]
	if kind == "write" && !model && !head && !edits && !hasReset {
		entry, err := tx.AppendEntry(c.id, draft)
		if err != nil {
			return 0, nil, err
		}
		record, err := create("done", entry.ID, "")
		return record.ID, nil, err
	}
	liveDoc, err := builtin(tx, c.id, "pi.live")
	if err != nil {
		return 0, nil, err
	}
	live, err := liveDoc.Get()
	if err != nil {
		return 0, nil, err
	}
	run := generationObject(live["run"])
	busy := run != nil
	if kind == "input" && input.Type == "steer" && busy {
		if trace := submitTraceFromContext(ctx); trace != nil && trace.extract != nil && trace.span.AddLink != nil {
			record, found, err := tx.Task(submissionEntryID(run["taskId"]))
			if err != nil {
				return 0, nil, err
			}
			if found {
				if carrier, ok := record.Trace.(goai.TelemetryCarrier); ok {
					trace.runLink = copyTelemetryCarrier(&carrier)
				}
			}
		}
	}
	if busy && (kind == "continue" || kind == "input" && input.WhenBusy == "reject") {
		return 0, nil, &ConversationBusy{ConversationID: c.id}
	}
	if !busy && kind == "input" && input.WhenIdle == "reject" {
		return 0, nil, &ConversationIdle{ConversationID: c.id}
	}
	var boundary *canonicalBoundary
	if !busy {
		boundary, err = prepareCanonicalBoundary(tx, c.id, settings)
		if err != nil {
			return 0, nil, err
		}
	}
	if kind == "continue" {
		record, err := create("placed", 0, "")
		if err != nil {
			return 0, nil, err
		}
		users, first, _, err := boundary.apply(tx, true, now)
		if err != nil {
			return 0, nil, err
		}
		failureFirst := submissionEntryID(generationObject(live["runFailure"])["first"])
		if failureFirst != 0 {
			first = failureFirst
		}
		users = append(users, record.ID)
		if _, err := canonicalStartRun(tx, c.id, live, users, first, false); err != nil {
			return 0, nil, err
		}
		return record.ID, nil, liveDoc.Set(live)
	}
	if boundary == nil || len(boundary.items) > 0 {
		record, err := create("queued", 0, "")
		if err != nil {
			return 0, nil, err
		}
		item := JSON{"id": record.ID, "mode": "write", "entry": draft}
		if kind == "input" {
			mode := "followUp"
			if input.Type == "steer" {
				mode = "steer"
			}
			item = JSON{"id": record.ID, "mode": mode, "content": content}
			if input.Entry != nil {
				metadata := JSON{"kind": input.Entry.Kind}
				if input.Entry.HasData {
					metadata["data"] = input.Entry.Data
				}
				item["entry"] = metadata
			}
		}
		items = append(items, item)
		inboxValue["items"] = items
		if err := inbox.Set(inboxValue); err != nil {
			return 0, nil, err
		}
		if boundary == nil {
			return record.ID, nil, nil
		}
		boundary.items = items
		users, first, _, err := boundary.apply(tx, true, now)
		if err != nil {
			return 0, nil, err
		}
		if len(users) > 0 {
			if _, err := canonicalStartRun(tx, c.id, live, users, first, false); err != nil {
				return 0, nil, err
			}
		}
		return record.ID, nil, liveDoc.Set(live)
	}
	if kind == "write" {
		if target := submissionEntryID(draft["head"]); target != 0 && boundary.head != 0 && target < boundary.head {
			record, err := create("unanswered", 0, "stale")
			return record.ID, nil, err
		}
		entry, err := tx.AppendEntry(c.id, draft)
		if err != nil {
			return 0, nil, err
		}
		record, err := create("done", entry.ID, "")
		return record.ID, nil, err
	}
	user := JSON{"kind": userEntryKind, "model": []any{JSON{"role": "user", "content": content, "timestamp": now}}}
	if input.Entry != nil {
		user["kind"] = input.Entry.Kind
		if input.Entry.HasData {
			user["data"] = input.Entry.Data
		}
	}
	entry, err := tx.AppendEntry(c.id, user)
	if err != nil {
		return 0, nil, err
	}
	record, err := create("placed", entry.ID, "")
	if err != nil {
		return 0, nil, err
	}
	if _, err := canonicalStartRun(tx, c.id, live, []ID{record.ID}, entry.ID, false); err != nil {
		return 0, nil, err
	}
	return record.ID, nil, liveDoc.Set(live)
}

func (h *Harness) Submission(ctx context.Context, id ID) (*SubmissionHandle, error) {
	if h.closing.Load() {
		return nil, ErrClosed
	}
	s, e := h.session.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := s.Submissions[id]; !ok {
		return nil, nil
	}
	return &SubmissionHandle{h, id}, nil
}
func (s *SubmissionHandle) ID() ID { return s.id }
func (s *SubmissionHandle) Wait(ctx context.Context) (Settlement, error) {
	return s.wait(ctx, nil)
}
func (s *SubmissionHandle) wait(ctx context.Context, binding *TaskRuntime) (Settlement, error) {
	if binding != nil {
		if err := binding.check(); err != nil {
			return Settlement{}, err
		}
	}
	if ctx == nil {
		return Settlement{}, reject("nil context")
	}
	if binding == nil {
		if e := s.h.Resume(ctx); e != nil {
			return Settlement{}, e
		}
	} else {
		combined, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(binding.context, cancel)
		defer func() { stop(); cancel() }()
		ctx = combined
		// Recheck on the Session line before granting progress authority.
		if err := s.h.session.readTasks(ctx, func(Snapshot) error {
			if err := binding.check(); err != nil {
				return err
			}
			if s.h.closing.Load() {
				return ErrClosed
			}
			s.h.scheduler.enable()
			return nil
		}); err != nil {
			return Settlement{}, err
		}
	}
	wake := make(chan struct{}, 1)
	registered := false
	defer func() {
		if registered {
			s.h.session.taskBookkeeping(func() { s.h.removeSubmissionWaiter(s.id, wake) })
		}
	}()
	for {
		var result Settlement
		terminal := false
		e := s.h.session.readTasks(ctx, func(state Snapshot) error {
			if binding != nil {
				if err := binding.check(); err != nil {
					return err
				}
			}
			sub, ok := state.Submissions[s.id]
			if !ok {
				return reject("unknown submission")
			}
			terminal = terminalStatus(sub.Status)
			if !terminal {
				if s.h.closing.Load() {
					return ErrClosed
				}
				if !registered {
					if err := s.h.addSubmissionWaiter(s.id, wake); err != nil {
						return err
					}
					registered = true
				}
				return nil
			}
			value, err := copyObject(sub.Value, s.h.session.limits)
			if err != nil {
				return err
			}
			sub.Value = value
			result.Submission = sub
			result.Record = publicSubmission(sub)
			if number, ok := exactNumber(sub.Value["taskId"]); ok && number != nil && number.IsInt() {
				if selected, ok := state.Tasks[ID(number.Num().Uint64())]; ok {
					result.Task, err = copyTask(selected, s.h.session.limits)
					if err != nil {
						return err
					}
				}
			}
			if result.Task.ID == 0 && result.Record.Status == "unanswered" {
				for _, doc := range state.Documents {
					if doc.Kind != "pi.live" || doc.Owner != sub.Conversation || doc.Retired {
						continue
					}
					failure := generationObject(doc.Value["runFailure"])
					inputs, _ := failure["inputs"].([]any)
					for _, input := range inputs {
						if submissionEntryID(input) == s.id {
							if selected, ok := state.Tasks[submissionEntryID(failure["taskId"])]; ok {
								result.Task, err = copyTask(selected, s.h.session.limits)
								if err != nil {
									return err
								}
							}
						}
					}
				}
			}
			for _, id := range ids(state.Tasks) {
				if result.Task.ID != 0 {
					break
				}
				task := state.Tasks[id]
				if task.Kind != "pi.generation" {
					continue
				}
				var cp generationCheckpoint
				if fromObject(task.Checkpoint, &cp, s.h.session.limits) == nil && generationIncludesSubmission(cp, s.id) && cp.Phase != "queued" && cp.Successor == 0 {
					result.Task, err = copyTask(task, s.h.session.limits)
					if err != nil {
						return err
					}
					break
				}
			}
			if result.Record.Answer != 0 {
				if entry, ok := state.Entries[result.Record.Answer]; ok && len(entry.Model) > 0 {
					messages, e := detachReceipts(entry.Model, s.h.session.limits)
					if e != nil {
						return e
					}
					result.Message = &messages[0]
				}
				for _, task := range state.Tasks {
					if task.Kind != "pi.generation" || task.Conversation != sub.Conversation {
						continue
					}
					record, e := CanonicalTask(task, s.h.session.limits)
					if e != nil {
						return e
					}
					if record.State.Outcome != nil && record.State.Outcome.Result != nil && submissionEntryID(generationObject(record.State.Outcome.Result.Value)["entryId"]) == result.Record.Answer {
						result.Task, err = copyTask(task, s.h.session.limits)
						if err != nil {
							return err
						}
						break
					}
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
			if binding != nil {
				if err := binding.check(); err != nil {
					return Settlement{}, err
				}
			}
			return result, nil
		}
		select {
		case <-ctx.Done():
			return Settlement{}, ctx.Err()
		case <-s.h.life.Done():
			if failure := s.h.session.failure(); failure != nil {
				return Settlement{}, failure
			}
			return Settlement{}, ErrClosed
		case <-wake:
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
	_, e := h.session.Commit(ctx, func(tx *Tx) (any, error) {
		if err := tx.touchKeys(ALL_KEYS); err != nil {
			return nil, err
		}
		if h.closing.Load() {
			return nil, ErrClosed
		}
		sub, ok := tx.state.Submissions[s.id]
		if !ok {
			return nil, reject("unknown submission")
		}
		if terminalStatus(sub.Status) {
			return nil, nil
		}
		var task Task
		found := false
		for _, t := range tx.state.Tasks {
			if t.Kind != "pi.generation" {
				continue
			}
			var cp generationCheckpoint
			if e := fromObject(t.Checkpoint, &cp, h.session.limits); e != nil {
				return nil, e
			}
			if cp.Submission == s.id {
				if cp.Phase != "queued" || cp.InputEntry != 0 {
					return nil, reject("submission already placed")
				}
				if len(h.scheduler.ownedLive(tx.state, t.ID)) > 0 {
					return nil, reject("queued submission owns live work; abort its conversation")
				}
				task = t
				cp.Phase = "terminal"
				value, e := dtoObject(cp, h.session.limits)
				if e != nil {
					return nil, e
				}
				task.Checkpoint = value
				task.Status = "aborted"
				found = true
				break
			}
		}
		if !found {
			return nil, reject("queued task unavailable")
		}
		if tx.taskTerminals == nil {
			tx.taskTerminals = map[ID]bool{}
		}
		tx.taskTerminals[task.ID] = true
		if e := tx.putTaskRecord(task); e != nil {
			return nil, e
		}
		sub.Status = "aborted"
		sub.Value = JSON{"errorCode": "withdrawn"}
		if e := tx.putSubmissionRecord(sub); e != nil {
			return nil, e
		}
		inbox, e := builtin(tx, sub.Conversation, "pi.inbox")
		if e != nil {
			return nil, e
		}
		return nil, inbox.Update(func(v JSON) error {
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

func (h *Harness) conversationGenerationBusy(state Snapshot, id ID) bool {
	for _, doc := range state.Documents {
		if doc.Kind == "pi.live" && !doc.Retired && doc.Owner == id && len(generationObject(doc.Value["run"])) != 0 {
			return true
		}
	}
	// Legacy hosts can have request work without a run document. Queued work
	// and already-decided held predecessors do not make admission busy.
	for _, task := range state.Tasks {
		if task.Kind != "pi.generation" || task.Conversation != id || terminalStatus(task.Status) || taskHasDecidedOutcome(task) {
			continue
		}
		var cp generationCheckpoint
		if h.scheduler.invocations[task.ID] != nil || task.Status == "running" || fromObject(task.Checkpoint, &cp, h.session.limits) != nil || cp.Phase != "queued" {
			return true
		}
	}
	return false
}

func inputReceipt(text string, blocks []goai.ContentBlock, limits Limits) (MessageReceipt, error) {
	if blocks == nil {
		return MessageReceipt{Role: goai.RoleUser, ContentText: &text}, nil
	}
	content := []goai.ContentBlock{}
	if text != "" {
		content = append(content, goai.ContentBlock{Type: "text", Text: text})
	}
	content = append(content, blocks...)
	return contributionReceipt(goai.Message{Role: goai.RoleUser, Content: content}, limits)
}
