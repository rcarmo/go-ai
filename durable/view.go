package durable

import (
	"context"
	"sync"
)

// ConversationView is the committed structural mount, not provider context.
// Entries include the current head marker and its retained raw suffix; Docs
// contains only pi.agent, pi.live, pi.inbox and pi.usage singleton documents.
type ConversationView struct {
	Conversation Conversation    `json:"conversation"`
	Entries      []Entry         `json:"entries"`
	Docs         map[string]JSON `json:"docs"`
}

func mountedKind(kind string) bool {
	switch kind {
	case "pi.agent", "pi.live", "pi.inbox", "pi.usage":
		return true
	}
	return false
}
func buildConversationView(state Snapshot, id ID, limits Limits) (ConversationView, error) {
	conversation, ok := state.Conversations[id]
	if !ok {
		return ConversationView{}, reject("unknown conversation")
	}
	context, err := deriveContextView(state, id, 0, limits)
	if err != nil {
		return ConversationView{}, err
	}
	value := ConversationView{Conversation: conversation, Entries: context.Entries, Docs: map[string]JSON{}}
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
func copyConversationView(value ConversationView, limits Limits) (ConversationView, error) {
	copy := ConversationView{Conversation: value.Conversation, Entries: make([]Entry, len(value.Entries)), Docs: make(map[string]JSON, len(value.Docs))}
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
func (c *ConversationHandle) View(ctx context.Context) (ConversationView, error) {
	var value ConversationView
	err := c.h.session.readTasks(ctx, func(state Snapshot) error {
		if c.h.closing.Load() {
			return ErrClosed
		}
		var err error
		value, err = buildConversationView(state, c.id, c.h.session.limits)
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
}

func (c *ConversationHandle) Watch(ctx context.Context) (*ConversationWatch, error) {
	if c.h.closing.Load() {
		return nil, ErrClosed
	}
	state, sub, err := c.h.session.SubscribeCommits(ctx)
	if err != nil {
		return nil, err
	}
	value, err := buildConversationView(state, c.id, c.h.session.limits)
	if err != nil || c.h.closing.Load() {
		sub.Stop()
		if err == nil {
			err = ErrClosed
		}
		return nil, err
	}
	return &ConversationWatch{current: value, sub: sub, limits: c.h.session.limits}, nil
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
			next, err = buildConversationView(*frame.Snapshot, before.Conversation.ID, w.limits)
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
				if entry.Head == 0 {
					next.Entries = append(next.Entries, entry)
				} else {
					kept := []Entry{entry}
					for _, old := range next.Entries {
						if old.Head == 0 && old.ID >= entry.Head {
							kept = append(kept, old)
						}
					}
					next.Entries = kept
				}
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
