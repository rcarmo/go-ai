package durable

import (
	"context"
	"sort"
)

// A queued projection owns a bounded window, never a complete history snapshot.
// Values/ops are immutable internally; callback values are detached at delivery.
type conversationPublicationFrame struct {
	value    ConversationViewCopy
	readonly ConversationView
	ops      []Operation
	err      error
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

func advanceConversationWindow(before ConversationViewCopy, publication CommitPublication, limits Limits, bounds conversationWindowBounds) (ConversationViewCopy, []Entry, int, error) {
	next, err := copyConversationView(before, limits)
	if err != nil {
		return ConversationViewCopy{}, nil, 0, err
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
				return ConversationViewCopy{}, nil, 0, err
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
			return ConversationViewCopy{}, nil, 0, err
		}
		appended = append(appended, entry)
	}
	sort.Slice(appended, func(i, j int) bool { return appended[i].ID < appended[j].ID })
	next.Entries = append(next.Entries, appended...)
	drop := 0
	if len(next.Entries) > bounds.max {
		drop = len(next.Entries) - bounds.min
		next.Before = next.Entries[drop-1].ID
		kept := make([]Entry, bounds.min)
		copy(kept, next.Entries[drop:])
		next.Entries = kept
	}
	return next, appended, drop, nil
}

// Keep Source's append -> trim -> before sequence, including inserted entries
// immediately discarded by this publication. Native preparation supplies the
// incarnation-aware document log; callers without one retain the bounded diff.
func conversationWindowOperations(before, after JSON, appended []Entry, drop int, limits Limits, documents ...[]Operation) ([]Operation, error) {
	fallback := func() ([]Operation, error) { return diffOperations(before, after, limits) }
	var ops []Operation
	var err error
	if len(documents) > 0 {
		ops = documents[0]
	} else {
		ops, err = diffOperations(JSON{"conversation": before["conversation"], "docs": before["docs"]}, JSON{"conversation": after["conversation"], "docs": after["docs"]}, limits)
		if err != nil {
			return nil, err
		}
	}
	// The document-only diff can itself overflow to a root. That root lacks
	// entries/before, so it must be rebuilt from the complete view envelope.
	if len(ops) > 0 && ops[0][0] == "r" {
		return fallback()
	}
	checkIntermediate := drop > 0 || len(ops) > 0
	if len(appended) > 0 {
		// Strict decoding requires an object root. This envelope is smaller
		// and shallower than the actual splice tuple; it uses the record,
		// not document, byte limit because trimmed inserts may be larger.
		records := make([]any, len(appended))
		for i, entry := range appended {
			object, err := conversationEntryObject(entry, limits)
			if err != nil {
				return fallback()
			}
			records[i] = object
		}
		encoded, err := encodeBounded(JSON{"entries": records}, limits, limits.MaxRecordBytes)
		if err != nil {
			return fallback()
		}
		var inserted struct {
			Entries []any `json:"entries"`
		}
		if err = decodeStrict(encoded, limits, limits.MaxRecordBytes, &inserted); err != nil {
			return fallback()
		}
		oldEntries := before["entries"].([]any)
		ops = append(ops, Operation{"p", []any{"entries"}, len(oldEntries), 0, inserted.Entries})
		if drop > 0 {
			ops = append(ops, Operation{"p", []any{"entries"}, 0, drop, []any{}}, Operation{"s", []any{"before"}, after["before"]})
		}
	}
	// Do not widen native DTO/operation or intermediate-document limits to
	// represent Source's transient append. Fall back to the existing bounded
	// final-value diff if that transient cannot be replayed under this policy.
	owned, err := ownOperations(ops, limits)
	if err != nil {
		return fallback()
	}
	if checkIntermediate {
		if _, err = ApplyOperations(before, owned, limits); err != nil {
			return fallback()
		}
	}
	return owned, nil
}

// Session-line preparation must never advance the head or fail its transaction
// because a listener's projection cannot be represented. Such errors are queued
// for this watch only, behind the same post-publication readiness fence.
func (w *ConversationWatch) preparePublication(ctx context.Context, publication CommitPublication, ready <-chan struct{}, shared ...map[ID]*ImmutableValue) *preparedSubscription {
	w.mu.Lock()
	before, mounted, failed := w.head, w.incarnations, w.projectionErr
	readonlyBefore := w.readonlyHead
	w.mu.Unlock()
	if failed != nil || !conversationPublicationRelevant(publication, before.Conversation.ID) {
		return nil
	}
	frameContext := context.WithoutCancel(ctx)
	failure := func(err error) *preparedSubscription {
		frame := PublicationFrame{Context: frameContext, ready: ready, conversation: &conversationPublicationFrame{err: err}}
		return &preparedSubscription{sub: w.sub, frame: frame, reset: frame, size: 1, resetSize: 1}
	}
	docs, incarnations, documentOps, err := projectConversationDocuments(before, mounted, publication, w.limits)
	if err != nil {
		return failure(err)
	}
	entries := publication
	entries.Documents = nil // adapter/raw delivery retains its existing projection
	next, appended, drop, err := advanceConversationWindow(before, entries, w.limits, w.window)
	if err != nil {
		return failure(err)
	}
	next.Docs = docs
	a, err := dtoObject(before, w.limits)
	if err != nil {
		return failure(err)
	}
	b, err := dtoObject(next, w.limits)
	if err != nil {
		return failure(err)
	}
	ops, err := conversationWindowOperations(a, b, appended, drop, w.limits, documentOps)
	if err != nil {
		return failure(err)
	}
	var sharedDocs map[ID]*ImmutableValue
	if len(shared) > 0 {
		sharedDocs = shared[0]
	}
	readonly, err := advanceReadonlyConversation(readonlyBefore, next, appended, drop, documentOps, w.limits, sharedDocs)
	if err != nil {
		return failure(err)
	}
	if len(ops) == 0 {
		// An unrepresentable net-zero batch may still replace incarnations.
		// Adopt that witness after receipt without consuming a delivery slot.
		frame := PublicationFrame{Context: frameContext, ready: ready, conversation: &conversationPublicationFrame{readonly: readonly, value: next}}
		return &preparedSubscription{sub: w.sub, frame: frame, view: &next, incarnations: incarnations, headOnly: true}
	}
	root := []Operation{{"r", map[string]any(b)}}
	frame := PublicationFrame{Context: frameContext, ready: ready, conversation: &conversationPublicationFrame{readonly: readonly, value: next, ops: ops}}
	reset := PublicationFrame{Context: frameContext, ready: ready, conversation: &conversationPublicationFrame{readonly: readonly, value: next, ops: root}}
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
	return &preparedSubscription{sub: w.sub, frame: frame, reset: reset, size: size, resetSize: resetSize, view: &next, incarnations: incarnations}
}

// Called only after receipt adoption, with the subscription lock held. This is
// infallible assignment; callbacks and their errors stay off the mutation line.
func (w *ConversationWatch) adoptPublication(item preparedSubscription) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if item.disableProjection {
		w.projectionDisabled = true
	}
	if item.frame.conversation != nil && item.frame.conversation.err != nil {
		w.projectionErr = item.frame.conversation.err
		return
	}
	if item.view != nil {
		w.head = *item.view
		if item.frame.conversation != nil {
			w.readonlyHead = item.frame.conversation.readonly
		} else if item.reset.conversation != nil {
			w.readonlyHead = item.reset.conversation.readonly
		}
		w.incarnations = item.incarnations
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
	var next ConversationViewCopy
	var err error
	if frame.Snapshot != nil {
		bounds := w.window
		bounds.hasFrom = false
		next, err = buildConversationWindow(*frame.Snapshot, before.Conversation.ID, w.limits, bounds)
	} else {
		next, _, _, err = advanceConversationWindow(before, frame.Publication, w.limits, w.window)
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
		detached, err = freezeConversationView(next, w.limits)
		if err == nil {
			w.current = next
			w.readonlyCurrent = detached
		}
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
