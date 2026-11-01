package durable

import (
	"context"
	"sort"
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
	mu      sync.Mutex
	current ConversationView
	sub     *CommitSubscription
	limits  Limits
	window  conversationWindowBounds
}

func (c *ConversationHandle) Watch(ctx context.Context, window ...ConversationWindow) (*ConversationWatch, error) {
	if c.h.closing.Load() {
		return nil, ErrClosed
	}
	bounds, err := resolveConversationWindow(window)
	if err != nil {
		return nil, err
	}
	state, sub, err := c.h.session.SubscribeCommits(ctx)
	if err != nil {
		return nil, err
	}
	value, err := buildConversationWindow(state, c.id, c.h.session.limits, bounds)
	if err != nil || c.h.closing.Load() {
		sub.Stop()
		if err == nil {
			err = ErrClosed
		}
		return nil, err
	}
	return &ConversationWatch{current: value, sub: sub, limits: c.h.session.limits, window: bounds}, nil
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
		w.mu.Lock()
		before := w.current
		next, err := copyConversationView(before, w.limits)
		reset := frame.Snapshot != nil
		if err == nil && reset {
			bounds := w.window
			bounds.hasFrom = false // from only constrains initial attachment
			next, err = buildConversationWindow(*frame.Snapshot, before.Conversation.ID, w.limits, bounds)
		}
		if err == nil && !reset {
			for _, change := range frame.Publication.Documents {
				doc := change.Record
				if doc.Scope != "conversation" || doc.Owner != before.Conversation.ID || doc.Family || !mountedKind(doc.Kind) {
					continue
				}
				if change.Value == nil || doc.Retired {
					delete(next.Docs, doc.Kind)
				} else {
					next.Docs[doc.Kind], err = copyObject(change.Value, w.limits)
					if err != nil {
						break
					}
				}
			}
			appended := []Entry{}
			for _, write := range frame.Publication.Tables {
				if err != nil {
					break
				}
				if write.Conversation != nil && write.Conversation.ID == before.Conversation.ID {
					next.Conversation = *write.Conversation
				}
				if write.Entry == nil || write.Entry.Conversation != before.Conversation.ID {
					continue
				}
				entry, e := copyEntry(*write.Entry, w.limits)
				if e != nil {
					err = e
					break
				}
				appended = append(appended, entry)
			}
			sort.Slice(appended, func(i, j int) bool { return appended[i].ID < appended[j].ID })
			next.Entries = append(next.Entries, appended...)
			if len(next.Entries) > w.window.max {
				drop := len(next.Entries) - w.window.min
				next.Before = next.Entries[drop-1].ID
				kept := make([]Entry, w.window.min)
				copy(kept, next.Entries[drop:])
				next.Entries = kept
			}
		}
		var ops []Operation
		if err == nil {
			a, e := dtoObject(before, w.limits)
			err = e
			if err == nil {
				b, e := dtoObject(next, w.limits)
				err = e
				if err == nil {
					if reset {
						ops = []Operation{{"r", map[string]any(b)}}
					} else {
						ops, err = diffOperations(a, b, w.limits)
					}
				}
			}
		}
		if err == nil {
			w.current = next
		}
		detached, e := copyConversationView(next, w.limits)
		if err == nil {
			err = e
		}
		w.mu.Unlock()
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			return nil
		}
		return listener(ctx, detached, ops)
	})
}
