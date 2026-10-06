package durable

import "context"

// Commit exposes the Session commit surface without conversation binding or
// scheduling. ConversationHandle.Commit supplies the bound form.
func (h *Harness) Commit(ctx context.Context, change func(*Tx) error) (uint64, error) {
	return h.CommitTasks(ctx, 0, change)
}
func (h *Harness) TypedEntry(ctx context.Context, def *EntryDefinition, conversation, id ID) (Entry, bool, error) {
	if h.closing.Load() {
		return Entry{}, false, ErrClosed
	}
	return h.session.TypedEntry(ctx, def, conversation, id)
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

// SnapshotDefinition forwards a detached, read-only typed Session snapshot.
// Reads admitted before Close retain Session ordering; fresh reads reject.
func (h *Harness) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (JSON, bool, error) {
	if h.closing.Load() {
		return nil, false, ErrClosed
	}
	return h.session.SnapshotDefinition(ctx, def, owner, key)
}
func (h *Harness) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, conversation ID, key *string, at ID) (JSON, bool, error) {
	if h.closing.Load() {
		return nil, false, ErrClosed
	}
	return h.session.SnapshotDefinitionAsOf(ctx, def, conversation, key, at)
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
func (c *ConversationHandle) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, key *string) (JSON, bool, error) {
	owner, err := c.documentOwner(def)
	if err != nil {
		return nil, false, err
	}
	return c.h.SnapshotDefinition(ctx, def, owner, key)
}
func (c *ConversationHandle) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, key *string, at ID) (JSON, bool, error) {
	if _, err := c.documentOwner(def); err != nil {
		return nil, false, err
	}
	return c.h.SnapshotDefinitionAsOf(ctx, def, c.id, key, at)
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
