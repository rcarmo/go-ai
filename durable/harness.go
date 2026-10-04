package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync"
	"sync/atomic"
	"time"
)

// Harness runs persistent model generations and their owned host tools. Opening never starts
// provider effects. Explicit Submit/Resume/Wait may schedule committed work.
type Harness struct {
	session     *Session
	options     Options
	life        context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	closing     atomic.Bool
	workers     map[ID]bool
	pins        map[ID]map[string]registeredTool
	invocations map[ID]context.CancelFunc
	wg          sync.WaitGroup
	closeOnce   sync.Once
	closeDone   chan struct{}
	closeErr    error
	changed     chan struct{}
}
type ConversationHandle struct {
	h  *Harness
	id ID
}
type agentState struct {
	Model        ModelRef        `json:"model"`
	Name         string          `json:"name"`
	SystemPrompt string          `json:"systemPrompt"`
	Settings     RequestSettings `json:"settings"`
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
	h := &Harness{session: s, options: options, life: life, cancel: cancel, workers: map[ID]bool{}, pins: map[ID]map[string]registeredTool{}, invocations: map[ID]context.CancelFunc{}, closeDone: make(chan struct{}), changed: make(chan struct{})}
	state, e := s.Snapshot(ctx)
	if e != nil {
		cancel()
		_ = s.Close(context.Background())
		return nil, e
	}
	var recovered []Task
	for _, task := range state.Tasks {
		if (task.Kind == "pi.generation" || task.Kind == "pi.tool") && task.Status == "running" {
			task.Status = "pending"
			recovered = append(recovered, task)
		}
	}
	if len(recovered) > 0 {
		_, e = s.Commit(ctx, func(tx *Tx) error {
			for _, task := range recovered {
				if e := tx.PutTask(task); e != nil {
					return e
				}
			}
			return nil
		})
		if e != nil {
			cancel()
			_ = s.Close(context.Background())
			return nil, e
		}
	}
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
	return &ConversationHandle{h, 1}, nil
}
func (h *Harness) Conversation(ctx context.Context, id ID) (*ConversationHandle, error) {
	s, e := h.session.Snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := s.Conversations[id]; !ok {
		return nil, reject("unknown conversation")
	}
	return &ConversationHandle{h, id}, nil
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
	return &ConversationHandle{h, id}, nil
}
func (h *Harness) agent(c AgentChange) (agentState, error) {
	if c.Model.ID == "" || c.Model.Provider == "" {
		return agentState{}, reject("model reference required")
	}
	settings, e := cloneSettings(c.Settings, h.session.limits)
	if e == nil && (c.Model.ID == "" || len(c.Model.ID) > h.session.limits.MaxStringBytes || len(c.SystemPrompt) > h.session.limits.MaxStringBytes) {
		return agentState{}, reject("agent string limit")
	}
	return agentState{c.Model, c.Name, c.SystemPrompt, settings}, e
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
	return c.h.session.Commit(ctx, callback)
}
func (h *Harness) Snapshot(ctx context.Context) (Snapshot, error) { return h.session.Snapshot(ctx) }
func (h *Harness) Inspect(ctx context.Context) (Snapshot, error)  { return h.Snapshot(ctx) }
func terminalStatus(status string) bool {
	return status == "done" || status == "failed" || status == "aborted"
}
func (h *Harness) Resume(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing.Load() {
		return ErrClosed
	}
	s, e := h.session.Snapshot(ctx)
	if e != nil {
		return e
	}
	for _, t := range s.Tasks {
		if t.Kind == "pi.generation" && !terminalStatus(t.Status) {
			h.scheduleLocked(t.Conversation)
		}
	}
	return nil
}
func (h *Harness) scheduleLocked(id ID) {
	if h.closing.Load() || h.workers[id] {
		return
	}
	h.workers[id] = true
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		drained := h.runConversation(id)
		h.mu.Lock()
		delete(h.workers, id)
		// An admission can race the worker's empty-queue check. Reconcile under
		// the same mutex used by Submit so a late queued task is never stranded.
		if drained && !h.closing.Load() && h.life.Err() == nil {
			if _, _, ok, e := h.nextTask(id); e == nil && ok {
				h.scheduleLocked(id)
			}
		}
		close(h.changed)
		h.changed = make(chan struct{})
		h.mu.Unlock()
	}()
}
func (h *Harness) Close(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	h.closeOnce.Do(func() {
		h.closing.Store(true)
		h.cancel()
		go func() {
			h.mu.Lock()
			// Join the admission mutex before Wait so any pre-fence worker Add
			// is settled. Closing is already atomic and forbids subsequent Adds.
			h.closing.Store(true)
			h.mu.Unlock()
			h.wg.Wait()
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
func (h *Harness) WaitForIdle(ctx context.Context) error {
	if e := h.Resume(ctx); e != nil {
		return e
	}
	for {
		signal := h.signal()
		s, e := h.Snapshot(ctx)
		if e != nil {
			return e
		}
		busy := false
		for _, t := range s.Tasks {
			if !terminalStatus(t.Status) {
				busy = true
				break
			}
		}
		if !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		case <-time.After(10 * time.Millisecond):
		}
	}
}
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
