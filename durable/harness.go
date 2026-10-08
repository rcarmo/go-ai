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
	session           *Session
	scheduler         *taskScheduler
	options           Options
	hostSettings      atomic.Pointer[HarnessSettings]
	life              context.Context
	cancel            context.CancelFunc
	mu                sync.Mutex
	closing           atomic.Bool
	workers           map[ID]bool
	pins              map[ID]map[string]registeredTool
	closeOnce         sync.Once
	closeDone         chan struct{}
	closeErr          error
	changed           chan struct{}
	submissionWaiters map[ID]map[chan struct{}]bool // Session-line owned; bounded registrations.
}
type ConversationHandle struct {
	h  *Harness
	id ID
}
type agentState struct {
	Model           ModelRef                `json:"model"`
	Name            string                  `json:"name"`
	Cwd             string                  `json:"cwd,omitempty"`
	SystemPrompt    string                  `json:"systemPrompt"`
	Instructions    *string                 `json:"instructions,omitempty"`
	Settings        RequestSettings         `json:"settings"`
	ThinkingLevel   goai.ModelThinkingLevel `json:"thinkingLevel,omitempty"`
	Extensions      *[]string               `json:"extensions,omitempty"`
	Tools           *[]string               `json:"tools,omitempty"`
	ExtensionFilter *ExtensionFilter        `json:"extensionFilter,omitempty"`
	ToolsRemoved    *[]string               `json:"toolsRemoved,omitempty"`
	// Process-local defaults are injected only for selection, never persisted.
	defaultExtensions *[]string
}

func (h *Harness) selectionAgent(agent agentState) agentState {
	agent.defaultExtensions = h.options.Extensions
	if settings := h.hostSettings.Load(); settings != nil {
		agent.defaultExtensions = settings.Extensions
	}
	return agent
}

func Open(ctx context.Context, store Storage, options Options) (*Harness, error) {
	if ctx == nil {
		return nil, reject("nil context")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if options.Settings != nil {
		resolved := (&Harness{options: options}).resolvedSettings(RequestSettings{})
		value, err := cloneSettings(resolved, DefaultLimits())
		if err != nil {
			return nil, err
		}
		enabledRetry, enabledCompact := value.Retry.Enabled, value.Compaction.Enabled
		extensions := options.Settings.Extensions
		if extensions != nil {
			names, err := agentSelectionNames(*extensions)
			if err != nil {
				return nil, err
			}
			extensions = &names
		}
		options.Settings = &HarnessSettings{ContextRetentionMs: copyTaskTime(value.ContextRetentionMs), Extensions: extensions, Stream: value, Retry: &RetrySettings{Enabled: &enabledRetry, MaxRetries: &value.Retry.MaxRetries, BaseDelayMs: &value.Retry.BaseDelayMs, MaxDelayMs: &value.Retry.MaxDelayMs}, Compaction: &CompactionSettings{Enabled: &enabledCompact, ReserveTokens: &value.Compaction.ReserveTokens, KeepRecentTokens: &value.Compaction.KeepRecentTokens, BackgroundTokens: &value.Compaction.BackgroundTokens, MaxTokens: &value.Compaction.MaxTokens}, ToolExecution: value.ToolExecution, SteeringMode: value.SteeringMode, FollowUpMode: value.FollowUpMode, Progress: mergeProgress(nil, value.Progress)}
	}
	if options.Extensions != nil {
		names, err := agentSelectionNames(*options.Extensions)
		if err != nil {
			return nil, err
		}
		options.Extensions = &names
	}
	s, e := OpenSessionWithOptions(store, SessionOptions{Now: options.Now})
	if e != nil {
		return nil, e
	}
	life, cancel := context.WithCancel(context.Background())
	if options.Catalog != nil {
		options.Models = options.Catalog.GetModel
	}
	if options.Models == nil {
		options.Models = goai.GetModel
	}
	h := &Harness{session: s, options: options, life: life, cancel: cancel, workers: map[ID]bool{}, pins: map[ID]map[string]registeredTool{}, closeDone: make(chan struct{}), changed: make(chan struct{})}
	if options.Settings != nil && options.Settings.Extensions == nil {
		options.Settings.Extensions = options.Extensions
	}
	h.hostSettings.Store(options.Settings)
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
	if err = h.options.Registry.registerBuiltinTask(compaction); err != nil {
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
func (h *Harness) Root(ctx context.Context, change AgentChange) (*ConversationHandle, error) {
	return h.RootWithInit(ctx, change, nil)
}

// RootWithInit initializes the reserved root once. Existing roots ignore both
// the convenience agent change and initializer, matching lazy root acquisition.
func (h *Harness) RootWithInit(ctx context.Context, change AgentChange, init ConversationInit) (*ConversationHandle, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return nil, ErrClosed
	}
	if e := h.configureLockedWithInit(ctx, 1, change, true, init); e != nil {
		return nil, e
	}
	return &ConversationHandle{h: h, id: 1}, nil
}
func (h *Harness) Conversation(ctx context.Context, id ID) (*ConversationHandle, error) {
	if h.closing.Load() {
		return nil, ErrClosed
	}
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
	return h.createConversation(ctx, change, nil)
}

func (h *Harness) createConversation(ctx context.Context, change AgentChange, init func(*Tx, ID) error) (*ConversationHandle, error) {
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
		v, e := dtoObject(state, h.session.limits)
		if e != nil {
			return e
		}
		{
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			if existing, ok := agentDocument(candidate, id); ok {
				handle, err := tx.Document(existing.ID)
				if err != nil {
					return err
				}
				e = handle.Set(v)
			} else {
				e = reject("created agent unavailable")
			}
		}
		if e != nil {
			return e
		}
		if err := initializeBuiltins(tx, id); err != nil {
			return err
		}
		if init != nil {
			return callConversationInit(init, tx, id)
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return &ConversationHandle{h: h, id: id}, nil
}
func (h *Harness) agent(c AgentChange) (agentState, error) {
	if (c.Model.ID == "") != (c.Model.Provider == "") {
		return agentState{}, reject("incomplete model reference")
	}
	switch c.ThinkingLevel {
	case "", "off", "minimal", "low", "medium", "high", "xhigh":
	default:
		return agentState{}, reject("invalid thinking level")
	}
	settings, e := cloneSettings(c.Settings, h.session.limits)
	if e == nil && (len(c.Model.ID) > h.session.limits.MaxStringBytes || len(c.SystemPrompt) > h.session.limits.MaxStringBytes || len(c.Cwd) > h.session.limits.MaxStringBytes) {
		return agentState{}, reject("agent string limit")
	}
	state := agentState{Model: c.Model, Name: c.Name, Cwd: c.Cwd, SystemPrompt: c.SystemPrompt, Settings: settings, ThinkingLevel: c.ThinkingLevel}
	if c.Instructions != nil {
		if len(*c.Instructions) > h.session.limits.MaxStringBytes {
			return agentState{}, reject("agent string limit")
		}
		value := *c.Instructions
		state.Instructions = &value
	}
	for _, selection := range []struct {
		source *[]string
		target **[]string
	}{{c.Extensions, &state.Extensions}, {c.Tools, &state.Tools}} {
		if selection.source == nil {
			continue
		}
		names, err := agentSelectionNames(*selection.source)
		if err != nil {
			return agentState{}, err
		}
		*selection.target = &names
	}
	if c.Extensions != nil && c.ExtensionFilter != nil || c.Tools != nil && c.ToolsRemoved != nil {
		return agentState{}, reject("conflicting agent selection")
	}
	if c.ExtensionFilter != nil {
		value := *c.ExtensionFilter
		var err error
		value.Add, err = agentSelectionNames(value.Add)
		if err != nil {
			return agentState{}, err
		}
		value.Remove, err = agentSelectionNames(value.Remove)
		if err != nil {
			return agentState{}, err
		}
		state.ExtensionFilter = &value
	}
	if c.ToolsRemoved != nil {
		names, err := agentSelectionNames(*c.ToolsRemoved)
		if err != nil {
			return agentState{}, err
		}
		state.ToolsRemoved = &names
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
	return h.configureLockedWithInit(ctx, id, c, initializeOnly, nil)
}

func (h *Harness) configureLockedWithInit(ctx context.Context, id ID, c AgentChange, initializeOnly bool, init ConversationInit) error {
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
		firstRoot := initializeOnly && id == 1 && !found
		if firstRoot {
			for _, retained := range tx.state.Documents {
				if retained.Scope == "conversation" && retained.Owner == id && retained.Kind == "pi.agent" {
					firstRoot = false
					break
				}
			}
		}
		if firstRoot {
			if err := h.initializeCreatedConversation(tx, tx.state.Conversations[id]); err != nil {
				return err
			}
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			created, ok := agentDocument(candidate, id)
			if !ok {
				return reject("created agent unavailable")
			}
			handle, err := tx.Document(created.ID)
			if err != nil {
				return err
			}
			if err := handle.Set(value); err != nil {
				return err
			}
			if init != nil {
				return callConversationInit(init, tx, id)
			}
			return nil
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

// Context returns current or as-of provider context. Zero/omitted at means current.
func (c *ConversationHandle) Context(ctx context.Context, cutoff ...ID) (*goai.Context, error) {
	if len(cutoff) > 1 {
		return nil, reject("multiple context cutoffs")
	}
	var at ID
	if len(cutoff) == 1 {
		at = cutoff[0]
	}
	if c.h.closing.Load() {
		return nil, ErrClosed
	}
	var a agentState
	var view ContextView
	e := c.h.session.readTasks(ctx, func(state Snapshot) error {
		if _, ok := state.Conversations[c.id]; !ok {
			return reject("unknown conversation")
		}
		if d, ok := agentDocument(state, c.id); ok {
			if err := fromObject(d.Value, &a, c.h.session.limits); err != nil {
				return err
			}
		}
		var err error
		view, err = c.h.session.cachedContextView(state, c.id, at)
		return err
	})
	if e != nil {
		return nil, e
	}
	conv := &goai.Context{SystemPrompt: a.SystemPrompt}
	for _, m := range view.Messages {
		conv.Messages = append(conv.Messages, receiptMessage(m))
	}
	return conv, nil
}
func (c *ConversationHandle) ContextView(ctx context.Context, at ID) (ContextView, error) {
	if c.h.closing.Load() {
		return ContextView{}, ErrClosed
	}
	return c.h.session.ContextView(ctx, c.id, at)
}

// ScanEntries pages the fork-visible history in the selected ID order.
func (c *ConversationHandle) ScanEntries(ctx context.Context, q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	// Conversation history defaults oldest-first, while direct Storage scans
	// default newest-first. Continuing without order follows its cursor.
	if q.Order == "" && cursor == "" {
		q.Order = Ascending
	}
	if q.Conversation != 0 && q.Conversation != c.id {
		return Page[Entry]{}, reject("entry query conversation mismatch")
	}
	q.Conversation = c.id
	if c.h.closing.Load() {
		return Page[Entry]{}, ErrClosed
	}
	return c.h.session.ScanEntries(ctx, q, limit, cursor)
}

func (c *ConversationHandle) Entries(ctx context.Context, cursor EntryCursor, limit int) ([]Entry, error) {
	if c.h.closing.Load() {
		return nil, ErrClosed
	}
	return c.h.session.store.Entries(ctx, c.id, cursor, limit)
}

// Commit binds task/document creation to this conversation without enabling
// scheduling. Committed application changes are allowed while work is live.
func (c *ConversationHandle) Commit(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	return c.h.CommitTasks(ctx, c.id, callback)
}
func (h *Harness) Snapshot(ctx context.Context) (Snapshot, error) { return h.session.Snapshot(ctx) }

// Inspect returns live work without scheduling, migration, or callbacks.
// Snapshot is the separate raw storage inspection surface.
func (h *Harness) Inspect(ctx context.Context) (HarnessInspection, error) { return h.InspectTasks(ctx) }
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
	// Newly initialized plain-Session conversations have no committed baseline.
	// Build a candidate only for that miss; existing handles read staged edits.
	if len(tx.writes) > 0 {
		candidate, err := tx.current()
		if err != nil {
			return nil, err
		}
		for _, d := range candidate.Documents {
			if !d.Retired && d.Scope == "conversation" && d.Owner == conversation && d.Kind == kind && d.Key == "" {
				return tx.Document(d.ID)
			}
		}
	}
	return nil, reject("builtin document missing")
}
func initializeBuiltins(tx *Tx, conversation ID) error {
	for _, v := range []struct {
		kind  string
		value JSON
	}{{"pi.live", JSON{}}, {"pi.inbox", JSON{"items": []any{}}}, {"pi.usage", JSON{"models": JSON{}, "tools": JSON{}}}} {
		if tx.session != nil && tx.session.taskScheduler != nil {
			found := false
			for _, doc := range tx.state.Documents {
				if doc.Scope == "conversation" && doc.Owner == conversation && doc.Kind == v.kind && !doc.Retired {
					found = true
					break
				}
			}
			if !found && len(tx.writes) > 0 {
				candidate, err := tx.current()
				if err != nil {
					return err
				}
				for _, doc := range candidate.Documents {
					if doc.Scope == "conversation" && doc.Owner == conversation && doc.Kind == v.kind && !doc.Retired {
						found = true
						break
					}
				}
			}
			if found {
				continue
			}
		}
		id, e := tx.MintID()
		if e != nil {
			return e
		}
		if _, e = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: conversation, Kind: v.kind, Version: 1, Value: v.value}); e != nil {
			return e
		}
	}
	if tx.session != nil && tx.session.taskScheduler != nil {
		_, err := ensureProviderDocument(tx, conversation)
		return err
	}
	return nil
}

// CommitTasks binds native task creation to an optional conversation.
// Execution replacements still require private runtime actions.
func (h *Harness) CommitTasks(ctx context.Context, conversation ID, callback func(*Tx) error) (uint64, error) {
	if h.closing.Load() {
		return 0, ErrClosed
	}
	return h.session.Commit(ctx, func(tx *Tx) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if conversation != 0 {
			if _, ok := tx.state.Conversations[conversation]; !ok {
				return reject("unknown conversation")
			}
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
	result := TaskInspectionView{Scheduling: "paused", Tasks: []TaskInspection{}, Submissions: []Submission{}}
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
		for _, id := range ids(state.Submissions) {
			submission := state.Submissions[id]
			if submission.Status == "pending" || submission.Status == "running" {
				copy := submission
				value, err := copyObject(submission.Value, h.session.limits)
				if err != nil {
					return err
				}
				copy.Value = value
				result.Submissions = append(result.Submissions, copy)
			}
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
		oldValue := steer.Value
		steer.Value = JSON{"taskId": task.ID}
		if checkpoint.Reset {
			steer.Status, steer.Value["errorCode"] = "aborted", "reset"
		}
		retainSubmissionPlacement(oldValue, steer.Value, hold.Entry)
		if value != nil {
			steer.Value["message"] = value
		}
		if err := tx.PutSubmission(steer); err != nil {
			return err
		}
	}
	sub, ok := tx.state.Submissions[hold.Submission]
	if !ok {
		return reject("submission unavailable")
	}
	if !terminalStatus(sub.Status) {
		oldValue := sub.Value
		sub.Status = hold.FinalStatus
		if checkpoint.Reset {
			sub.Status = "aborted" // native envelope projects unanswered/reset
			sub.Value = JSON{"errorCode": "reset", "message": value}
		} else if hold.Action == "generation-no-model" {
			sub.Value = JSON{"errorCode": "no_model"}
		} else if hold.Action == "generation-model-failure" {
			sub.Value = JSON{"errorCode": "model_error"}
		} else if hold.Action == "generation-abort" {
			sub.Value = JSON{"errorCode": "aborted"}
			if value != nil {
				sub.Value["message"] = value
			}
		} else if hold.Action == "scheduler-generation" {
			sub.Value = JSON{"errorCode": hold.Outcome.Status, "reason": hold.Outcome.Reason}
			if hold.Entry != 0 {
				sub.Value["message"] = value
			}
		} else {
			sub.Value = JSON{"message": value}
			if hold.Outcome.Status == "failed" && hold.Outcome.Error != nil && hold.Outcome.Error.Detail != nil {
				var detail map[string]any
				switch value := hold.Outcome.Error.Detail.Value.(type) {
				case JSON:
					detail = value
				case map[string]any:
					detail = value
				}
				if detail["reason"] == "model_error" {
					sub.Value["errorCode"] = "model_error"
				}
			}
		}
		sub.Value["taskId"] = task.ID
		retainSubmissionPlacement(oldValue, sub.Value, hold.Entry)
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
		if err = live.Update(func(value JSON) error {
			delete(value, "run")
			delete(value, "generation")
			delete(value, "tools")
			return nil
		}); err != nil {
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
		// Fresh decisions already ran endRun. Only legacy held receipts with
		// pending inputs need boundary cleanup here; repeating it could place
		// writes queued for a later live run.
		if !terminalStatus(tx.state.Submissions[hold.Submission].Status) {
			if err := h.cleanupGeneration(tx, task, hold, entry.Value); err != nil {
				return err
			}
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
	// Stored scheduler-tool holds retain their legacy receipt cleanup. New
	// scheduler-tool-missing holds settle without inventing transcript records;
	// context derivation supplies the missing-result envelope to the provider.
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
