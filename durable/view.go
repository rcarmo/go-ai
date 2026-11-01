package durable

import (
	"context"
	"sync"
)

// ConversationView is a committed transcript window, independent of model context.
// Docs contains pi.agent, pi.live, pi.inbox, pi.provider and pi.usage singletons.
// Before is the preceding transcript entry ID, omitted at the transcript start.
type ConversationView struct {
	Conversation Conversation    `json:"conversation"`
	Entries      []Entry         `json:"entries"`
	Before       ID              `json:"before,omitempty"`
	Docs         map[string]JSON `json:"docs"`
}

func mountedKind(kind string) bool {
	switch kind {
	case "pi.agent", "pi.live", "pi.inbox", "pi.provider", "pi.usage":
		return true
	}
	return false
}

// buildConversationBase owns the metadata/documents common to transcript and
// legacy event projections, without deriving a model context as a side effect.
func buildConversationBase(state Snapshot, id ID, limits Limits) (ConversationView, error) {
	conversation, ok := state.Conversations[id]
	if !ok {
		return ConversationView{}, reject("unknown conversation")
	}
	value := ConversationView{Conversation: conversation, Entries: []Entry{}, Docs: map[string]JSON{}}
	for _, doc := range state.Documents {
		if doc.Scope == "conversation" && doc.Owner == id && !doc.Retired && !doc.Family && mountedKind(doc.Kind) {
			copy, err := copyObject(doc.Value, limits)
			if err != nil {
				return ConversationView{}, err
			}
			value.Docs[doc.Kind] = copy
		}
	}
	return value, nil
}

// Legacy event snapshots still pair active-head entries with model receipts.
// Transcript windows must not silently change that separate event API.
func buildActiveConversationView(state Snapshot, id ID, limits Limits) (ConversationView, error) {
	if _, ok := state.Conversations[id]; !ok {
		return ConversationView{}, reject("unknown conversation")
	}
	active, err := deriveContextView(state, id, 0, limits)
	if err != nil {
		return ConversationView{}, err
	}
	value, err := buildConversationBase(state, id, limits)
	if err != nil {
		return ConversationView{}, err
	}
	if active.Entries != nil {
		value.Entries = active.Entries
	}
	return value, nil
}

func buildConversationView(state Snapshot, id ID, limits Limits, options ...ConversationWindow) (ConversationView, error) {
	bounds, err := resolveConversationWindow(options)
	if err != nil {
		return ConversationView{}, err
	}
	return buildConversationWindow(state, id, limits, bounds)
}

func copyConversationView(value ConversationView, limits Limits) (ConversationView, error) {
	copy := ConversationView{Conversation: value.Conversation, Before: value.Before, Entries: make([]Entry, len(value.Entries)), Docs: make(map[string]JSON, len(value.Docs))}
	var err error
	for i, entry := range value.Entries {
		copy.Entries[i], err = copyEntry(entry, limits)
		if err != nil {
			return ConversationView{}, err
		}
	}
	for kind, doc := range value.Docs {
		copy.Docs[kind], err = copyObject(doc, limits)
		if err != nil {
			return ConversationView{}, err
		}
	}
	return copy, nil
}
func (c *ConversationHandle) View(ctx context.Context, window ...ConversationWindow) (ConversationView, error) {
	var value ConversationView
	err := c.h.session.readTasks(ctx, func(state Snapshot) error {
		if c.h.closing.Load() {
			return ErrClosed
		}
		var err error
		value, err = buildConversationView(state, c.id, c.h.session.limits, window...)
		return err
	})
	return value, err
}

// ConversationWatch shares the Session's bounded committed subscription. Its
// baseline is acquired atomically with registration. Overflow emits a root
// replacement; listeners are serial and off the mutation line.
type ConversationWatch struct {
	mu                 sync.Mutex
	current            ConversationView
	sub                *CommitSubscription
	limits             Limits
	window             conversationWindowBounds
	head               ConversationView // publication-time window; current remains last delivered
	incarnations       map[string]conversationIncarnation
	projectionDisabled bool  // wrappers retain authoritative raw resnapshots
	projectionErr      error // failed projections end only this watch, after readiness
}

func (c *ConversationHandle) Watch(ctx context.Context, window ...ConversationWindow) (*ConversationWatch, error) {
	if c.h.closing.Load() {
		return nil, ErrClosed
	}
	bounds, err := resolveConversationWindow(window)
	if err != nil {
		return nil, err
	}
	var watch *ConversationWatch
	_, sub, err := c.h.session.subscribeCommits(ctx, func(state Snapshot, p *CommitSubscription) error {
		value, err := buildConversationWindow(state, c.id, c.h.session.limits, bounds)
		if err != nil {
			return err
		}
		watch = &ConversationWatch{current: value, head: value, incarnations: conversationIncarnations(state, c.id), sub: p, limits: c.h.session.limits, window: bounds, projectionDisabled: !c.h.session.hasNativeReadImage()}
		p.conversation = watch
		return nil
	})
	if err != nil {
		return nil, err
	}
	if c.h.closing.Load() {
		sub.Stop()
		return nil, ErrClosed
	}
	return watch, nil
}
func (w *ConversationWatch) Value() (ConversationView, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return copyConversationView(w.current, w.limits)
}
func (w *ConversationWatch) Closed() <-chan struct{} { return w.sub.Closed() }
func (w *ConversationWatch) End() (WatchEnd, bool)   { return w.sub.End() }
func (w *ConversationWatch) Stop() WatchEnd          { return w.sub.Stop() }
func (w *ConversationWatch) Start(listener func(context.Context, ConversationView, []Operation) error) error {
	if listener == nil {
		return reject("nil conversation listener")
	}
	return w.sub.Start(func(ctx context.Context, frame PublicationFrame) error {
		if frame.conversation == nil {
			return w.deliverAuthoritative(ctx, frame, listener)
		}
		projection := frame.conversation
		if projection.err != nil {
			return projection.err
		}
		value, err := copyConversationView(projection.value, w.limits)
		if err != nil {
			return err
		}
		w.mu.Lock()
		w.current = projection.value
		w.mu.Unlock()
		return listener(ctx, value, projection.ops)
	})
}
