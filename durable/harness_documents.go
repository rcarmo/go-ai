package durable

import "context"

// Commit exposes the Session commit surface without conversation binding or
// scheduling. ConversationHandle.Commit supplies the bound form.
func (h *Harness) Commit(ctx context.Context, change func(*Tx) (any, error)) (any, error) {
	if change == nil {
		return nil, reject("nil transaction callback")
	}
	var value any
	_, err := h.commitTasks(ctx, 0, func(tx *Tx) error {
		var callbackErr error
		value, callbackErr = change(tx)
		return callbackErr
	})
	if err != nil {
		return nil, err
	}
	return value, nil
}

// Entry reads a global committed entry, as the pinned Harness reader does.
func (h *Harness) Entry(ctx context.Context, id ID) (Entry, bool, error) {
	if h.closing.Load() {
		return Entry{}, false, h.closedError()
	}
	stored, found, err := h.session.Entry(ctx, id)
	return stored.Entry, found, err
}
func (h *Harness) TypedEntry(ctx context.Context, def *EntryDefinition, id ID) (Entry, bool, error) {
	entry, found, err := h.Entry(ctx, id)
	if err != nil {
		return Entry{}, false, err
	}
	if def == nil {
		return Entry{}, false, reject("nil entry definition")
	}
	if !found || !def.Is(entry) {
		return Entry{}, false, nil
	}
	return entry, true, nil
}

// SubscribeCommits returns an atomic baseline and bounded serial subscription.
// Native callbacks run off-line, so they may perform subsequent Session work.
func (h *Harness) SubscribeCommits(ctx context.Context) (Snapshot, *CommitSubscription, error) {
	if h.closing.Load() {
		return Snapshot{}, nil, ErrClosed
	}
	state, sub, err := h.session.SubscribeCommits(ctx)
	if err == nil && h.closing.Load() {
		if sub != nil {
			sub.Stop()
		}
		return Snapshot{}, nil, ErrClosed
	}
	return state, sub, err
}

// SnapshotDefinition forwards a shared readonly typed Session snapshot.
// Reads admitted before Close retain Session ordering; fresh reads reject.
func (h *Harness) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (ReadonlyValue, bool, error) {
	if h.closing.Load() {
		return nil, false, ErrClosed
	}
	return h.session.SnapshotDefinition(ctx, def, owner, key)
}
func (h *Harness) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, conversation ID, key *string, at ID) (ReadonlyValue, bool, error) {
	if h.closing.Load() {
		return nil, false, ErrClosed
	}
	return h.session.SnapshotDefinitionAsOf(ctx, def, conversation, key, at)
}

// ObserveDefinition attaches a synchronous document observer. Callbacks run
// on the Session line and must not block or call Session APIs.
func (h *Harness) ObserveDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string, observer DocumentObserver) (*DocumentObservation, bool, error) {
	if h.closing.Load() {
		return nil, false, ErrClosed
	}
	observation, found, err := h.session.ObserveDefinition(ctx, def, owner, key, observer)
	if err == nil && h.closing.Load() {
		observation.Detach()
		return nil, false, ErrClosed
	}
	return observation, found, err
}

func (h *Harness) WatchDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (*DocumentWatch, bool, error) {
	if h.closing.Load() {
		return nil, false, ErrClosed
	}
	watch, found, err := h.session.WatchDefinition(ctx, def, owner, key)
	if err == nil && h.closing.Load() {
		if watch != nil {
			watch.Stop()
		}
		return nil, false, ErrClosed
	}
	return watch, found, err
}
func (h *Harness) DocumentState(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (*DocumentState, bool, error) {
	if h.closing.Load() {
		return nil, false, ErrClosed
	}
	state, found, err := h.session.DocumentState(ctx, def, owner, key)
	if err == nil && h.closing.Load() {
		if state != nil {
			state.Dispose()
		}
		return nil, false, ErrClosed
	}
	return state, found, err
}
func (h *Harness) UnloadDocuments(ctx context.Context) error {
	if h.closing.Load() {
		return ErrClosed
	}
	return h.session.UnloadDocuments(ctx)
}

// Conversation document forwarding binds conversation-scoped definitions to
// this handle; session-scoped definitions retain their ownerless address.
func (c *ConversationHandle) documentOwner(def *DocumentDefinition) (ID, error) {
	if def == nil {
		return 0, reject("invalid document definition token")
	}
	switch def.options.Scope {
	case "conversation":
		return c.id, nil
	case "session":
		return 0, nil
	default:
		return 0, reject("conversation document requires conversation or session scope")
	}
}
func (c *ConversationHandle) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, key *string) (ReadonlyValue, bool, error) {
	owner, err := c.documentOwner(def)
	if err != nil {
		return nil, false, err
	}
	return c.h.SnapshotDefinition(ctx, def, owner, key)
}
func (c *ConversationHandle) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, key *string, at ID) (ReadonlyValue, bool, error) {
	if _, err := c.documentOwner(def); err != nil {
		return nil, false, err
	}
	return c.h.SnapshotDefinitionAsOf(ctx, def, c.id, key, at)
}

// ObserveDefinition binds conversation-scoped and session-scoped documents.
func (c *ConversationHandle) ObserveDefinition(ctx context.Context, def *DocumentDefinition, key *string, observer DocumentObserver) (*DocumentObservation, bool, error) {
	owner, err := c.documentOwner(def)
	if err != nil {
		return nil, false, err
	}
	return c.h.ObserveDefinition(ctx, def, owner, key, observer)
}

func (c *ConversationHandle) WatchDefinition(ctx context.Context, def *DocumentDefinition, key *string) (*DocumentWatch, bool, error) {
	owner, err := c.documentOwner(def)
	if err != nil {
		return nil, false, err
	}
	return c.h.WatchDefinition(ctx, def, owner, key)
}
func (c *ConversationHandle) DocumentState(ctx context.Context, def *DocumentDefinition, key *string) (*DocumentState, bool, error) {
	owner, err := c.documentOwner(def)
	if err != nil {
		return nil, false, err
	}
	return c.h.DocumentState(ctx, def, owner, key)
}
