package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
)

// Harness runs persistent model generations and their owned host tools. Opening never starts
// provider effects. Explicit Submit/Resume/Wait may schedule committed work.
type Harness struct {
	session   *Session
	scheduler *taskScheduler
	options   Options
	life      context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closing   atomic.Bool
	workers   map[ID]bool
	pins      map[ID]map[string]registeredTool
	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
	changed   chan struct{}
}
type ConversationHandle struct {
	h  *Harness
	id ID
}
type agentState struct {
	Model        ModelRef        `json:"model"`
	Name         string          `json:"name"`
	Cwd          string          `json:"cwd,omitempty"`
	SystemPrompt string          `json:"systemPrompt"`
	Settings     RequestSettings `json:"settings"`
	Extensions   *[]string       `json:"extensions,omitempty"`
	Tools        *[]string       `json:"tools,omitempty"`
}

func Open(ctx context.Context, store Storage, options Options) (*Harness, error) {
	if ctx == nil {
		return nil, reject("nil context")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	s, e := OpenSession(store)
	if e != nil {
		return nil, e
	}
	life, cancel := context.WithCancel(context.Background())
	if options.Models == nil {
		options.Models = goai.GetModel
	}
	h := &Harness{session: s, options: options, life: life, cancel: cancel, workers: map[ID]bool{}, pins: map[ID]map[string]registeredTool{}, closeDone: make(chan struct{}), changed: make(chan struct{})}
	failOpen := func(primary error) (*Harness, error) {
		cancel()
		s.taskScheduler = nil
		// Owned cleanup must finish despite caller cancellation. Report its
		// secondary error only after Close has returned outside all lines;
		// retain the exact primary Open error and bounded callback policy.
		if closeErr := s.Close(context.Background()); closeErr != nil {
			reportTaskError(h.options.OnReport, closeErr)
		}
		return nil, primary
	}
	state, e := s.Snapshot(ctx)
	if e != nil {
		return failOpen(e)
	}
	var recovered []Task
	for _, id := range ids(state.Tasks) {
		task := state.Tasks[id]
		if (task.Kind == "pi.generation" || task.Kind == "pi.tool" || task.Execution != nil && task.Execution.Native != nil) && task.Status == "running" {
			owned, err := copyTask(task, s.limits)
			if err != nil {
				return failOpen(err)
			}
			task = owned
			task.Status = "pending"
			if task.Execution != nil && task.Execution.Native != nil {
				task.Execution.Native.State.Status = "pending"
			}
			recovered = append(recovered, task)
		}
	}
	// One confirmed running->pending transition per startup admission in ID
	// order, fitting persisted write/frame budgets without dispatching code.
	// Failure/cancellation closes ownership; already adopted prefix survives.
	// A later Open retries only the remaining running records.
	for _, task := range recovered {
		_, e = s.Commit(ctx, func(tx *Tx) error { return tx.stage(Write{Op: "put-task", Task: &task}) })
		if e != nil {
			return failOpen(e)
		}
	}
	h.scheduler = newTaskScheduler(h)
	s.taskScheduler = h.scheduler
	if options.Registry == nil {
		h.options.Registry = NewRegistry()
	}
	compaction, err := h.compactionDefinition()
	if err != nil {
		return failOpen(err)
	}
	if _, err = h.options.Registry.RegisterTask(compaction); err != nil {
		return failOpen(err)
	}
	anchor, err := backgroundAnchorDefinition()
	if err != nil {
		return failOpen(err)
	}
	if _, err = h.options.Registry.RegisterTask(anchor); err != nil {
		return failOpen(err)
	}
	reporter, err := subagentReporterDefinition()
	if err != nil {
		return failOpen(err)
	}
	if _, err = h.options.Registry.RegisterTask(reporter); err != nil {
		return failOpen(err)
	}
	h.scheduler.unsubscribe, e = h.options.Registry.subscribeTasks(h.scheduler.externalKick)
	if e != nil {
		return failOpen(e)
	}
	go h.scheduler.loop()
	return h, nil
}
func (h *Harness) notify() {
	h.mu.Lock()
	close(h.changed)
	h.changed = make(chan struct{})
	h.mu.Unlock()
}
func (h *Harness) signal() <-chan struct{} { h.mu.Lock(); defer h.mu.Unlock(); return h.changed }
func (h *Harness) Root(ctx context.Context, change AgentChange) (*ConversationHandle, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return nil, ErrClosed
	}
	if e := h.configureLocked(ctx, 1, change, true); e != nil {
		return nil, e
	}
	return &ConversationHandle{h: h, id: 1}, nil
}
func (h *Harness) Conversation(ctx context.Context, id ID) (*ConversationHandle, error) {
	s, e := h.session.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := s.Conversations[id]; !ok {
		return nil, reject("unknown conversation")
	}
	return &ConversationHandle{h: h, id: id}, nil
}
func (h *Harness) CreateConversation(ctx context.Context, change AgentChange) (*ConversationHandle, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return nil, ErrClosed
	}
	id, e := h.session.MintID(ctx)
	if e != nil {
		return nil, e
	}
	state, e := h.agent(change)
	if e != nil {
		return nil, e
	}
	_, e = h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if e := tx.CreateConversation(Conversation{ID: id, Name: change.Name}); e != nil {
			return e
		}
		doc, e := tx.MintID()
		if e != nil {
			return e
		}
		v, e := dtoObject(state, h.session.limits)
		if e != nil {
			return e
		}
		_, e = tx.CreateDocument(Document{ID: doc, Scope: "conversation", Owner: id, Kind: "pi.agent", Version: 1, Value: v, History: "rewindable", Fork: "asOf"})
		if e != nil {
			return e
		}
		return initializeBuiltins(tx, id)
	})
	if e != nil {
		return nil, e
	}
	return &ConversationHandle{h: h, id: id}, nil
}
func (h *Harness) agent(c AgentChange) (agentState, error) {
	if c.Model.ID == "" || c.Model.Provider == "" {
		return agentState{}, reject("model reference required")
	}
	settings, e := cloneSettings(c.Settings, h.session.limits)
	if e == nil && (c.Model.ID == "" || len(c.Model.ID) > h.session.limits.MaxStringBytes || len(c.SystemPrompt) > h.session.limits.MaxStringBytes || len(c.Cwd) > h.session.limits.MaxStringBytes) {
		return agentState{}, reject("agent string limit")
	}
	state := agentState{Model: c.Model, Name: c.Name, Cwd: c.Cwd, SystemPrompt: c.SystemPrompt, Settings: settings}
	for _, selection := range []struct {
		source *[]string
		target **[]string
	}{{c.Extensions, &state.Extensions}, {c.Tools, &state.Tools}} {
		if selection.source == nil {
			continue
		}
		if len(*selection.source) > DefaultLimits().MaxPage {
			return agentState{}, reject("agent selection limit")
		}
		copy := append([]string{}, (*selection.source)...)
		seen := map[string]bool{}
		for _, name := range copy {
			if !validKind(name) || seen[name] {
				return agentState{}, reject("invalid agent selection")
			}
			seen[name] = true
		}
		*selection.target = &copy
	}
	return state, e
}
func agentDocument(s Snapshot, id ID) (Document, bool) {
	for _, v := range s.Documents {
		if !v.Retired && v.Scope == "conversation" && v.Owner == id && v.Kind == "pi.agent" && v.Key == "" {
			return v, true
		}
	}
	return Document{}, false
}
func (h *Harness) configureLocked(ctx context.Context, id ID, c AgentChange, initializeOnly bool) error {
	s, e := h.session.Snapshot(ctx)
	if e != nil {
		return e
	}
	if _, ok := s.Conversations[id]; !ok {
		return reject("unknown conversation")
	}
	doc, found := agentDocument(s, id)
	if initializeOnly && found {
		return nil
	}
	state, e := h.agent(c)
	if e != nil {
		return e
	}
	value, e := dtoObject(state, h.session.limits)
	if e != nil {
		return e
	}
	_, e = h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if found {
			handle, e := tx.Document(doc.ID)
			if e != nil {
				return e
			}
			return handle.Set(value)
		}
		docID, e := tx.MintID()
		if e != nil {
			return e
		}
		_, e = tx.CreateDocument(Document{ID: docID, Scope: "conversation", Owner: id, Kind: "pi.agent", Version: 1, Value: value, History: "rewindable", Fork: "asOf"})
		if e != nil {
			return e
		}
		return initializeBuiltins(tx, id)
	})
	return e
}
func (c *ConversationHandle) ID() ID { return c.id }
func (c *ConversationHandle) Configure(ctx context.Context, change AgentChange) error {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	if c.h.closing.Load() {
		return ErrClosed
	}
	return c.h.configureLocked(ctx, c.id, change, false)
}
func (c *ConversationHandle) Context(ctx context.Context) (*goai.Context, error) {
	s, e := c.h.session.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	d, ok := agentDocument(s, c.id)
	if !ok {
		return nil, reject("agent not configured")
	}
	var a agentState
	if e = fromObject(d.Value, &a, c.h.session.limits); e != nil {
		return nil, e
	}
	messages, e := contextReceipts(s, c.id, c.h.session.limits)
	if e != nil {
		return nil, e
	}
	conv := &goai.Context{SystemPrompt: a.SystemPrompt}
	for _, m := range messages {
		conv.Messages = append(conv.Messages, receiptMessage(m))
	}
	return conv, nil
}
func (c *ConversationHandle) ContextView(ctx context.Context, at ID) (ContextView, error) {
	return c.h.session.ContextView(ctx, c.id, at)
}

func (c *ConversationHandle) Entries(ctx context.Context, cursor EntryCursor, limit int) ([]Entry, error) {
	return c.h.session.store.Entries(ctx, c.id, cursor, limit)
}

// Commit allows passive application records only while no generation owns this
// conversation. It does not trigger a model. Full scheduler ownership is M1c.
func (c *ConversationHandle) Commit(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	if c.h.closing.Load() {
		return 0, ErrClosed
	}
	s, e := c.h.session.Snapshot(ctx)
	if e != nil {
		return 0, e
	}
	for _, t := range s.Tasks {
		if t.Conversation == c.id && !terminalStatus(t.Status) {
			return 0, reject("conversation busy")
		}
	}
	return c.h.session.Commit(ctx, func(tx *Tx) error {
		if c.h.closing.Load() {
			return ErrClosed
		}
		return callback(tx)
	})
}
func (h *Harness) Snapshot(ctx context.Context) (Snapshot, error) { return h.session.Snapshot(ctx) }
func (h *Harness) Inspect(ctx context.Context) (Snapshot, error)  { return h.Snapshot(ctx) }
func terminalStatus(status string) bool {
	return status == "done" || status == "failed" || status == "aborted"
}
func (h *Harness) Resume(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.closing.Load() {
		return ErrClosed
	}
	h.scheduler.enable()
	return nil
}

// Submit wake seam only; no autonomous conversation dispatcher remains.
func (h *Harness) scheduleLocked(id ID) {
	if h.closing.Load() {
		return
	}
	h.workers[id] = true
	h.scheduler.enable()
}
func (h *Harness) Close(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	h.closeOnce.Do(func() {
		h.closing.Store(true)
		h.scheduler.sealed.Store(true)
		h.scheduler.unsubscribe()
		h.cancel()
		go func() {
			// Synchronise with an admitted submitter before releasing storage.
			h.mu.Lock()
			//lint:ignore SA2001 The empty section is an intentional mutex barrier.
			h.mu.Unlock()
			<-h.scheduler.done
			var joins []<-chan struct{}
			h.session.taskBookkeeping(func() {
				h.scheduler.notifyWaiters(ErrClosed)
				for _, r := range h.scheduler.invocations {
					h.scheduler.endOnLine(r)
					joins = append(joins, r.done)
				}
			})
			h.scheduler.dispatchStops()
			for _, done := range joins {
				<-done
			}
			// Actual host return can publish a retry barrier. Release process-only
			// failure tokens and errors only after all host bookkeeping has joined.
			h.session.taskBookkeeping(func() {
				h.scheduler.failed = map[ID]*TaskDefinition{}
				h.scheduler.failures = map[ID]error{}
				h.scheduler.retryAfter = map[ID]uint64{}
				h.scheduler.reconcileRetry = nil
				h.scheduler.reports = nil
			})
			h.mu.Lock()
			h.pins = map[ID]map[string]registeredTool{}
			h.mu.Unlock()
			h.closeErr = h.session.Close(context.Background())
			close(h.closeDone)
		}()
	})
	select {
	case <-h.closeDone:
		return h.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (h *Harness) WaitForIdle(ctx context.Context) error { return h.waitIdleScope(ctx, 0, nil) }
func contextReceipts(s Snapshot, id ID, l Limits) ([]messageReceipt, error) {
	view, e := deriveContextView(s, id, 0, l)
	return view.Messages, e
}

func builtin(tx *Tx, conversation ID, kind string) (*DocumentHandle, error) {
	for _, d := range tx.state.Documents {
		if !d.Retired && d.Scope == "conversation" && d.Owner == conversation && d.Kind == kind && d.Key == "" {
			return tx.Document(d.ID)
		}
	}
	return nil, reject("builtin document missing")
}
func initializeBuiltins(tx *Tx, conversation ID) error {
	for _, v := range []struct {
		kind  string
		value JSON
	}{{"pi.live", JSON{}}, {"pi.inbox", JSON{"items": []any{}}}, {"pi.usage", JSON{"models": JSON{}, "tools": JSON{}}}} {
		id, e := tx.MintID()
		if e != nil {
			return e
		}
		if _, e = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: conversation, Kind: v.kind, Version: 1, Value: v.value}); e != nil {
			return e
		}
	}
	return nil
}

// CommitTasks binds native task creation while preserving legacy passive busy
// Commit behaviour. Execution replacements still require private runtime actions.
func (h *Harness) CommitTasks(ctx context.Context, conversation ID, callback func(*Tx) error) (uint64, error) {
	if h.closing.Load() {
		return 0, ErrClosed
	}
	return h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if _, ok := tx.state.Conversations[conversation]; !ok {
			return reject("unknown conversation")
		}
		tx.taskConversation = conversation
		return callback(tx)
	})
}
func (h *Harness) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	var view TaskRecord
	var ok bool
	err := h.session.readTasks(ctx, func(state Snapshot) error {
		if h.closing.Load() {
			return ErrClosed
		}
		task, found := state.Tasks[id]
		ok = found
		if !found {
			return nil
		}
		var err error
		view, err = CanonicalTask(task, h.session.limits)
		return err
	})
	return view, ok, err
}
func (h *Harness) WaitForTask(ctx context.Context, id ID) (TaskRecord, error) {
	if err := h.Resume(ctx); err != nil {
		return TaskRecord{}, err
	}
	return h.waitTask(ctx, id, nil)
}
func (h *Harness) waitTask(ctx context.Context, id ID, binding *TaskRuntime) (TaskRecord, error) {
	if ctx == nil {
		return TaskRecord{}, reject("nil context")
	}
	wait := &taskWait{signal: make(chan struct{}, 1), target: id, binding: binding}
	defer func() {
		h.session.taskBookkeeping(func() { h.scheduler.releaseWaitClaims(wait); delete(h.scheduler.waiters, wait) })
		h.scheduler.kick()
	}()
	for {
		var view TaskRecord
		var terminal bool
		err := h.session.readTasks(ctx, func(state Snapshot) error {
			if h.closing.Load() {
				return ErrClosed
			}
			if binding != nil {
				if err := binding.check(); err != nil {
					return err
				}
			}
			task, ok := state.Tasks[id]
			if !ok {
				return reject("unknown task")
			}
			var err error
			view, err = CanonicalTask(task, h.session.limits)
			if err != nil {
				return err
			}
			terminal = terminalStatus(task.Status)
			if wait.failure != nil {
				return wait.failure
			}
			if terminal {
				return nil
			}
			if binding != nil {
				if err := h.scheduler.admitBoundWait(state, wait, id); err != nil {
					return err
				}
			}
			if !h.scheduler.waiters[wait] && len(h.scheduler.waiters) >= h.session.limits.MaxPage {
				return reject("task waiter capacity")
			}
			h.scheduler.waiters[wait] = true
			return nil
		})
		if err != nil {
			return TaskRecord{}, err
		}
		if terminal {
			return view, nil
		}
		select {
		case <-ctx.Done():
			return TaskRecord{}, ctx.Err()
		case <-h.life.Done():
			return TaskRecord{}, ErrClosed
		case <-wait.signal:
		}
	}
}
func (h *Harness) AbortTask(ctx context.Context, id ID) (string, error) {
	if err := h.Resume(ctx); err != nil {
		return "", err
	}
	result := "marked"
	var join <-chan struct{}
	var cancel context.CancelFunc
	_, err := h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		task, ok := tx.state.Tasks[id]
		if !ok {
			return reject("unknown task")
		}
		if terminalStatus(task.Status) {
			result = "terminal"
			return nil
		}
		if !taskManaged(task) {
			return reject("legacy raw task has no abort adapter")
		}
		if r := h.scheduler.invocations[id]; r != nil && !r.abortMode {
			join = r.done
			cancel = r.cancel
		}
		task = markTask(task)
		// Direct blocked native abort settles in THIS marking admission when
		// neither the task nor its ordinary owned work retains an actual host.
		// Foreign wait On is superseded by the mark and is not owned drain work.
		// Decided Holds and unevaluated migrations remain scheduler work.
		if task.Execution != nil && task.Execution.Native != nil && !taskHasDecidedOutcome(task) && h.scheduler.invocations[id] == nil && len(h.scheduler.ownedLive(tx.state, id)) == 0 {
			snapshot, err := h.options.Registry.taskSnapshot(tx.limits)
			if err != nil {
				return err
			}
			if reason := h.scheduler.nativeBlockedReason(task, snapshot); reason != "" {
				return h.scheduler.orphan(tx, task, reason)
			}
		}
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	if err != nil {
		return "", err
	}
	if cancel != nil {
		cancel()
	}
	if join != nil {
		select {
		case <-join:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	h.scheduler.kick()
	return result, nil
}
func (h *Harness) InspectTasks(ctx context.Context) (TaskInspectionView, error) {
	result := TaskInspectionView{Scheduling: "paused"}
	err := h.session.readTasks(ctx, func(state Snapshot) error {
		if h.closing.Load() {
			result.Scheduling = "closing"
			return ErrClosed
		}
		if h.scheduler.enabled.Load() {
			result.Scheduling = "running"
		}
		snapshot, err := h.options.Registry.taskSnapshot(h.session.limits)
		if err != nil {
			return err
		}
		for _, id := range ids(state.Tasks) {
			task := state.Tasks[id]
			if terminalStatus(task.Status) {
				continue
			}
			view, err := CanonicalTask(task, h.session.limits)
			if err != nil {
				return err
			}
			item := TaskInspection{Record: view, Kind: "ready"}
			if r := h.scheduler.invocations[id]; r != nil {
				item.Kind = "running"
			} else if taskHasDecidedOutcome(task) {
				item.Kind = "completing"
			} else {
				// Derive dependencies before definition fit, without migration,
				// dispatch or wake. A mark supersedes stale foreign wait members;
				// bottom-up abort also retains terminal-but-unreturned child hosts.
				if taskAborted(task) {
					item.On = h.scheduler.ownedLive(state, id)
					sort.Slice(item.On, func(i, j int) bool { return item.On[i] < item.On[j] })
				} else if view.State.Status == "waiting" {
					for _, member := range view.State.On {
						if child, exists := state.Tasks[member]; exists && !terminalStatus(child.Status) {
							item.On = append(item.On, member)
						}
					}
				}
				if len(item.On) > 0 {
					item.Kind = "waiting"
				} else if task.Execution != nil && task.Execution.Native != nil {
					if reason := h.scheduler.nativeBlockedReason(task, snapshot); reason != "" {
						item.Kind, item.Reason = "blocked", reason
					} else {
						item.Migrates = snapshot.Task(task.Kind).Version() > view.Version
					}
				} else if task.Kind != "pi.generation" && task.Kind != "pi.tool" {
					item.Kind = "blocked"
					item.Reason = "legacy_raw_task"
				}
			}
			result.Tasks = append(result.Tasks, item)
		}
		return nil
	})
	return result, err
}

func (h *Harness) cleanupGeneration(tx *Tx, task Task, hold *BuiltinTaskHold, value JSON) error {
	var checkpoint generationCheckpoint
	if err := fromObject(task.Checkpoint, &checkpoint, tx.limits); err != nil {
		return err
	}
	for _, id := range checkpoint.Steered {
		steer, exists := tx.state.Submissions[id]
		if !exists {
			return reject("steered submission missing")
		}
		if terminalStatus(steer.Status) {
			continue
		}
		steer.Status = hold.FinalStatus
		steer.Value = JSON{"message": value}
		if err := tx.PutSubmission(steer); err != nil {
			return err
		}
	}
	sub, ok := tx.state.Submissions[hold.Submission]
	if !ok {
		return reject("submission unavailable")
	}
	if !terminalStatus(sub.Status) {
		sub.Status = hold.FinalStatus
		if checkpoint.Reset {
			sub.Value = JSON{"errorCode": "reset", "message": value}
		} else if hold.Action == "scheduler-generation" {
			sub.Value = JSON{"errorCode": hold.Outcome.Status, "reason": hold.Outcome.Reason}
			if hold.Entry != 0 {
				sub.Value["message"] = value
			}
		} else {
			sub.Value = JSON{"message": value}
		}
		if err := tx.PutSubmission(sub); err != nil {
			return err
		}
	}
	live, err := builtin(tx, task.Conversation, "pi.live")
	if err != nil {
		return err
	}
	current, err := live.Get()
	if err != nil {
		return err
	}
	run, _ := current["run"].(map[string]any)
	if run != nil && fmt.Sprint(run["task"]) == strconv.FormatUint(uint64(task.ID), 10) {
		if err = live.Set(JSON{}); err != nil {
			return err
		}
	}
	if err := h.placeQueuedWrites(tx, task.Conversation); err != nil {
		return err
	}
	if _, err := h.placeQueuedResets(tx, task.Conversation); err != nil {
		return err
	}
	inbox, err := builtin(tx, task.Conversation, "pi.inbox")
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
			if fmt.Sprint(object["id"]) != strconv.FormatUint(uint64(hold.Submission), 10) {
				keep = append(keep, item)
			}
		}
		value["items"] = keep
		return nil
	})
}
func (h *Harness) finalizeBuiltinHold(tx *Tx, task Task) error {
	owned, err := copyTask(task, tx.limits)
	if err != nil {
		return err
	}
	task = owned
	hold := task.Execution.Builtin.Hold
	if hold == nil || hold.Stage != "held" {
		return reject("builtin hold not finalisable")
	}
	if hold.Action == "generation-receipt" {
		entry, ok := tx.state.Entries[hold.Entry]
		if !ok {
			return reject("hold receipt missing")
		}
		if err := h.cleanupGeneration(tx, task, hold, entry.Value); err != nil {
			return err
		}
	}
	if hold.Action == "scheduler-generation" {
		var cp generationCheckpoint
		if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		value := JSON{"errorCode": hold.Outcome.Status}
		if hold.Outcome.Status == "orphaned" {
			value["errorCode"] = hold.Outcome.Reason
		}
		if cp.Partial != nil {
			receipt := *cp.Partial
			receipt.StopReason = goai.StopReasonAborted
			receipt.ErrorCode = hold.Outcome.Status
			encoded, err := dtoObject(receipt, tx.limits)
			if err != nil {
				return err
			}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: encoded, ByTask: task.ID}); err != nil {
				return err
			}
			hold.Entry = id
			value = encoded
			usage, err := builtin(tx, task.Conversation, "pi.usage")
			if err != nil {
				return err
			}
			if err := usage.Update(func(v JSON) error { return addModelUsage(v, receipt, tx.limits) }); err != nil {
				return err
			}
			cp.Partial = nil
			checkpoint, err := dtoObject(cp, tx.limits)
			if err != nil {
				return err
			}
			task.Checkpoint = checkpoint
		}
		if err := h.cleanupGeneration(tx, task, hold, value); err != nil {
			return err
		}
	}
	if hold.Action == "scheduler-tool" {
		var cp toolCheckpoint
		if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		code := hold.Outcome.Status
		if hold.Outcome.Status == "orphaned" {
			code = hold.Outcome.Reason
		}
		text := cp.Output
		if text != "" {
			text += "\n"
		}
		text += code
		if len(text) > MaxToolOutputBytes {
			text = code
		}
		receipt := MessageReceipt{Role: goai.RoleToolResult, ToolCallID: cp.CallID, ToolName: cp.Offer.Name, Content: []goai.ContentBlock{{Type: "text", Text: text}}, IsError: true, ErrorCode: code, Details: JSON{}}
		value, err := dtoObject(receipt, tx.limits)
		if err != nil {
			return err
		}
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		if err := tx.AppendEntry(Entry{ID: id, Conversation: task.Conversation, Kind: "message", Value: value, ByTask: task.ID}); err != nil {
			return err
		}
		hold.Entry = id
		cp.Result = &receipt
		cp.ErrorCode = code
		checkpoint, err := dtoObject(cp, tx.limits)
		if err != nil {
			return err
		}
		task.Checkpoint = checkpoint
	}
	hold.Stage = "final"
	task.Status = hold.FinalStatus
	return tx.stage(Write{Op: "put-task", Task: &task})
}

// taskInScope walks ownership only, stopping at every retained background
// ancestor unless explicitly crossed. Direct conversation starts inside scope.
func taskInScope(state Snapshot, task Task, conversation ID, crossBackground bool) bool {
	if taskBackground(task) && !crossBackground {
		return false
	}
	current := task
	seen := map[ID]bool{}
	for {
		if current.Owner == 0 {
			if conversation == 0 && state.Conversations[current.Conversation].Owner == 0 {
				return true
			}
			if current.Conversation == conversation {
				return true
			}
		}
		owner := current.Owner
		if owner == 0 {
			owner = state.Conversations[current.Conversation].Owner
		}
		if owner == 0 || seen[owner] {
			return false
		}
		seen[owner] = true
		parent, ok := state.Tasks[owner]
		if !ok || taskBackground(parent) && !crossBackground {
			return false
		}
		current = parent
	}
}
func (c *ConversationHandle) WaitForIdle(ctx context.Context) error {
	return c.h.waitIdleScope(ctx, c.id, nil)
}
func (h *Harness) waitIdleScope(ctx context.Context, conversation ID, binding *TaskRuntime) error {
	if ctx == nil {
		return reject("nil context")
	}
	if binding != nil {
		if err := binding.check(); err != nil {
			return err
		}
		// Bound enable is checked and applied within its first idle admission.
		// A capability sealed while queued never creates an external retry epoch.
		return h.waitIdleAdmissionMode(ctx, conversation, binding, true)
	}
	if err := h.Resume(ctx); err != nil {
		return err
	}
	return h.waitIdleAdmission(ctx, conversation, nil)
}

func (h *Harness) waitIdleAdmission(ctx context.Context, conversation ID, binding *TaskRuntime) error {
	return h.waitIdleAdmissionMode(ctx, conversation, binding, false)
}
func (h *Harness) waitIdleAdmissionMode(ctx context.Context, conversation ID, binding *TaskRuntime, enable bool) error {
	rootWait := &taskWait{signal: make(chan struct{}, 1), binding: binding, idleScope: &conversation}
	defer func() {
		h.session.taskBookkeeping(func() {
			h.scheduler.releaseWaitClaims(rootWait)
			delete(h.scheduler.waiters, rootWait)
		})
		h.scheduler.kick()
	}()
	for {
		idle := true
		err := h.session.readTasks(ctx, func(state Snapshot) error {
			if h.closing.Load() {
				return ErrClosed
			}
			if binding != nil {
				if err := binding.check(); err != nil {
					return err
				}
			}
			if rootWait.failure != nil {
				return rootWait.failure
			}
			if enable {
				h.scheduler.enable()
				enable = false
			}
			targets := h.scheduler.idleTargets(state, conversation)
			idle = len(targets) == 0
			if idle {
				h.scheduler.releaseWaitClaims(rootWait)
				return nil
			}
			if !h.scheduler.waiters[rootWait] && len(h.scheduler.waiters) >= h.session.limits.MaxPage {
				return reject("idle waiter capacity")
			}
			if binding != nil {
				if err := h.scheduler.admitBoundTargets(state, rootWait, targets); err != nil {
					// Admission failed while still holding this original line: no
					// provisional ticket can escape into a reservation pass.
					h.scheduler.releaseWaitClaims(rootWait)
					delete(h.scheduler.waiters, rootWait)
					return err
				}
			}
			h.scheduler.waiters[rootWait] = true
			return nil
		})
		if err != nil {
			return err
		}
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.life.Done():
			return ErrClosed
		case <-rootWait.signal:
		}
	}
}
func (h *Harness) withdrawScopedInputs(tx *Tx, conversation ID, cross bool, cancelledOwners map[ID]bool) error {
	return h.withdrawScopedInputBatch(tx, conversation, cross, cancelledOwners, false)
}
func (h *Harness) withdrawScopedInputBatch(tx *Tx, conversation ID, cross bool, cancelledOwners map[ID]bool, one bool) error {
	affected := map[ID]bool{}
	state := tx.state
	var stateErr error
	if len(tx.writes) > 0 {
		state, stateErr = tx.current()
		if stateErr != nil {
			return stateErr
		}
	}
	for _, subID := range ids(state.Submissions) {
		sub := state.Submissions[subID]
		if sub.Type != "follow-up" && sub.Type != "steer" || terminalStatus(sub.Status) {
			continue
		}
		selected := false
		probe := Task{Conversation: sub.Conversation}
		if cancelledOwners == nil {
			selected = taskInScope(tx.state, probe, conversation, cross)
		} else {
			selected = taskBelowCancelled(state, probe)
		}
		if !selected {
			continue
		}
		queued := false
		for _, taskID := range ids(tx.state.Tasks) {
			task := tx.state.Tasks[taskID]
			if task.Kind != "pi.generation" {
				continue
			}
			var cp generationCheckpoint
			if fromObject(task.Checkpoint, &cp, tx.limits) != nil {
				continue
			}
			if cp.Submission == sub.ID && cp.Phase == "queued" {
				queued = true
				if len(h.scheduler.ownedLive(state, task.ID)) > 0 {
					// Keep the run/submission live while real descendants join;
					// scheduler abort dispatch later performs terminal cleanup.
					if !taskAborted(task) {
						task = markTask(task)
						if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
							return err
						}
					}
					if one && len(tx.writes) > 0 {
						return nil
					}
					queued = false
					break
				}
				task.Status = "aborted"
				cp.Abort = true
				cp.Phase = "terminal"
				checkpoint, err := dtoObject(cp, tx.limits)
				if err != nil {
					return err
				}
				task.Checkpoint = checkpoint
				if tx.taskTerminals == nil {
					tx.taskTerminals = map[ID]bool{}
				}
				tx.taskTerminals[task.ID] = true
				if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
					return err
				}
				break
			}
		}
		if !queued {
			continue
		}
		sub.Status = "aborted"
		sub.Value = JSON{"errorCode": "aborted"}
		if err := tx.PutSubmission(sub); err != nil {
			return err
		}
		affected[sub.Conversation] = true
		if one {
			break
		}
	}
	for id := range affected {
		inbox, err := builtin(tx, id, "pi.inbox")
		if err != nil {
			return err
		}
		if err := inbox.Update(func(value JSON) error {
			items, ok := value["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			keep := []any{}
			for _, item := range items {
				obj, ok := item.(map[string]any)
				if !ok {
					return reject("inbox item")
				}
				remove := false
				for _, sub := range tx.state.Submissions {
					if sub.Conversation == id && (sub.Type == "follow-up" || sub.Type == "steer") && fmt.Sprint(obj["id"]) == strconv.FormatUint(uint64(sub.ID), 10) {
						current, _ := tx.current()
						if terminalStatus(current.Submissions[sub.ID].Status) {
							remove = true
						}
					}
				}
				if !remove {
					keep = append(keep, item)
				}
			}
			value["items"] = keep
			return nil
		}); err != nil {
			return err
		}
		// Withdrawal is a final boundary too: queued passive writes survive
		// cancellation and are placed after the removed inputs.
		if err := h.placeQueuedWrites(tx, id); err != nil {
			return err
		}
	}
	return nil
}
