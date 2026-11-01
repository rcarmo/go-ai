package durable

import "context"

// Adapter recovery retains the publication-time window shape, but reads every
// retained entry/document body from the authoritative candidate. This head is
// bookkeeping only: it never enables exact-native delta projection.
func (w *ConversationWatch) prepareAuthoritativeReset(ctx context.Context, publication CommitPublication, state Snapshot, ready <-chan struct{}) (ConversationView, PublicationFrame, int64, error) {
	w.mu.Lock()
	before := w.head
	w.mu.Unlock()
	base, err := buildConversationBase(state, before.Conversation.ID, w.limits)
	if err != nil {
		return ConversationView{}, PublicationFrame{}, 0, err
	}
	layout := before
	layout.Docs = nil
	// A changed ancestry or missing/relocated retained record invalidates the
	// old shape. Rehydrate conservatively instead of resurrecting stale bodies.
	valid := base.Conversation == before.Conversation
	if before.Before != 0 {
		_, ok := state.Entries[before.Before]
		valid = valid && ok
	}
	layout.Entries = make([]Entry, len(before.Entries))
	for i, entry := range before.Entries {
		current, ok := state.Entries[entry.ID]
		if !ok || current.ID != entry.ID || current.Conversation != entry.Conversation || current.Seq != entry.Seq || current.Position != entry.Position {
			valid = false
			break
		}
		layout.Entries[i] = current
	}
	var next ConversationView
	if valid {
		entries := publication
		entries.Documents = nil
		// Project from authoritative entry payloads, not a stale private body.
		next, _, _, err = advanceConversationWindow(layout, entries, w.limits, w.window)
		next.Conversation = base.Conversation
		next.Docs = base.Docs
	} else {
		bounds := w.window
		bounds.hasFrom = false
		next, err = buildConversationWindow(state, before.Conversation.ID, w.limits, bounds)
	}
	if err != nil {
		return ConversationView{}, PublicationFrame{}, 0, err
	}
	body, err := dtoObject(next, w.limits)
	if err != nil {
		return ConversationView{}, PublicationFrame{}, 0, err
	}
	root, err := ownOperations([]Operation{{"r", map[string]any(body)}}, w.limits)
	if err != nil {
		return ConversationView{}, PublicationFrame{}, 0, err
	}
	valueBytes, err := encodeBounded(body, w.limits, w.limits.MaxDocumentBytes)
	if err != nil {
		return ConversationView{}, PublicationFrame{}, 0, err
	}
	opBytes, err := encodeBounded(root, w.limits, w.limits.MaxRecordBytes)
	if err != nil {
		return ConversationView{}, PublicationFrame{}, 0, err
	}
	frame := PublicationFrame{Context: context.WithoutCancel(ctx), ready: ready, conversation: &conversationPublicationFrame{value: next, ops: root}}
	return next, frame, int64(len(valueBytes) + len(opBytes)), nil
}
