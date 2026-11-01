package durable

import (
	"context"
	"sort"
)

// A queued projection owns a bounded window, never a complete history snapshot.
// Values/ops are immutable internally; callback values are detached at delivery.
type conversationPublicationFrame struct {
	value ConversationView
	ops   []Operation
	err   error
}

func conversationPublicationRelevant(publication CommitPublication, id ID) bool {
	for _, change := range publication.Documents {
		d := change.Record
		if d.Scope == "conversation" && d.Owner == id && !d.Family && mountedKind(d.Kind) {
			return true
		}
	}
	for _, write := range publication.Tables {
		if write.Conversation != nil && write.Conversation.ID == id || write.Entry != nil && write.Entry.Conversation == id {
			return true
		}
	}
	return false
}

func advanceConversationWindow(before ConversationView, publication CommitPublication, limits Limits, bounds conversationWindowBounds) (ConversationView, error) {
	next, err := copyConversationView(before, limits)
	if err != nil {
		return ConversationView{}, err
	}
	for _, change := range publication.Documents {
		d := change.Record
		if d.Scope != "conversation" || d.Owner != before.Conversation.ID || d.Family || !mountedKind(d.Kind) {
			continue
		}
		if change.Value == nil || d.Retired {
			delete(next.Docs, d.Kind)
		} else {
			next.Docs[d.Kind], err = copyObject(change.Value, limits)
			if err != nil {
				return ConversationView{}, err
			}
		}
	}
	appended := []Entry{}
	for _, write := range publication.Tables {
		if write.Conversation != nil && write.Conversation.ID == before.Conversation.ID {
			next.Conversation = *write.Conversation
		}
		if write.Entry == nil || write.Entry.Conversation != before.Conversation.ID {
			continue
		}
		entry, err := copyEntry(*write.Entry, limits)
		if err != nil {
			return ConversationView{}, err
		}
		appended = append(appended, entry)
	}
	sort.Slice(appended, func(i, j int) bool { return appended[i].ID < appended[j].ID })
	next.Entries = append(next.Entries, appended...)
	if len(next.Entries) > bounds.max {
		drop := len(next.Entries) - bounds.min
		next.Before = next.Entries[drop-1].ID
		kept := make([]Entry, bounds.min)
		copy(kept, next.Entries[drop:])
		next.Entries = kept
	}
	return next, nil
}

// Session-line preparation must never advance the head or fail its transaction
// because a listener's projection cannot be represented. Such errors are queued
// for this watch only, behind the same post-publication readiness fence.
func (w *ConversationWatch) preparePublication(ctx context.Context, publication CommitPublication, ready <-chan struct{}) *preparedSubscription {
	w.mu.Lock()
	before, failed := w.head, w.projectionErr
	w.mu.Unlock()
	if failed != nil || !conversationPublicationRelevant(publication, before.Conversation.ID) {
		return nil
	}
	frameContext := context.WithoutCancel(ctx)
	failure := func(err error) *preparedSubscription {
		frame := PublicationFrame{Context: frameContext, ready: ready, conversation: &conversationPublicationFrame{err: err}}
		return &preparedSubscription{sub: w.sub, frame: frame, reset: frame, size: 1, resetSize: 1}
	}
	next, err := advanceConversationWindow(before, publication, w.limits, w.window)
	if err != nil {
		return failure(err)
	}
	a, err := dtoObject(before, w.limits)
	if err != nil {
		return failure(err)
	}
	b, err := dtoObject(next, w.limits)
	if err != nil {
		return failure(err)
	}
	ops, err := diffOperations(a, b, w.limits)
	if err != nil {
		return failure(err)
	}
	if len(ops) == 0 {
		return nil
	}
	root := []Operation{{"r", map[string]any(b)}}
	frame := PublicationFrame{Context: frameContext, ready: ready, conversation: &conversationPublicationFrame{value: next, ops: ops}}
	reset := PublicationFrame{Context: frameContext, ready: ready, conversation: &conversationPublicationFrame{value: next, ops: root}}
	// Charge both retained graphs, but validate them independently as before.
	// A joint envelope would lower the effective node/depth limits. The root
	// tuple adds exactly eight bytes to its already-validated JSON value.
	valueEncoded, err := encodeBounded(b, w.limits, w.limits.MaxDocumentBytes)
	if err != nil {
		return failure(err)
	}
	opsEncoded, err := encodeBounded(ops, w.limits, w.limits.MaxRecordBytes)
	if err != nil {
		return failure(err)
	}
	size := int64(len(valueEncoded)) + int64(len(opsEncoded))
	resetSize := 2*int64(len(valueEncoded)) + 8
	return &preparedSubscription{sub: w.sub, frame: frame, reset: reset, size: size, resetSize: resetSize, view: &next}
}

// Called only after receipt adoption, with the subscription lock held. This is
// infallible assignment; callbacks and their errors stay off the mutation line.
func (w *ConversationWatch) adoptPublication(item preparedSubscription) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if item.disableProjection {
		w.projectionDisabled = true
		return
	}
	if item.frame.conversation.err != nil {
		w.projectionErr = item.frame.conversation.err
		return
	}
	if item.view != nil {
		w.head = *item.view
	}
}

// A wrapped Snapshot remains authoritative even at equal sequence. Once a
// wrapper publication is observed, this watch keeps the serial/raw path rather
// than trusting a head that predates the adapter's projection generation.
func (w *ConversationWatch) projectionStatus() (enabled, failed bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.projectionDisabled, w.projectionErr != nil
}

func (w *ConversationWatch) deliverAuthoritative(ctx context.Context, frame PublicationFrame, listener func(context.Context, ConversationView, []Operation) error) error {
	w.mu.Lock()
	before := w.current
	var next ConversationView
	var err error
	if frame.Snapshot != nil {
		bounds := w.window
		bounds.hasFrom = false
		next, err = buildConversationWindow(*frame.Snapshot, before.Conversation.ID, w.limits, bounds)
	} else {
		next, err = advanceConversationWindow(before, frame.Publication, w.limits, w.window)
	}
	var ops []Operation
	if err == nil {
		var a, b JSON
		a, err = dtoObject(before, w.limits)
		if err == nil {
			b, err = dtoObject(next, w.limits)
		}
		if err == nil {
			if frame.Snapshot != nil {
				ops = []Operation{{"r", map[string]any(b)}}
			} else {
				ops, err = diffOperations(a, b, w.limits)
			}
		}
	}
	var detached ConversationView
	if err == nil {
		w.current = next
		detached, err = copyConversationView(next, w.limits)
	}
	w.mu.Unlock()
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}
	return listener(ctx, detached, ops)
}
