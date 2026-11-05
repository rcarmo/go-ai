package durable

import "context"

// ReadLine is a callback-scoped read capability over one committed Session
// image. Returned records are detached; escaped or overlapping calls reject.
// It exposes no storage mutation or allocator capability.
type ReadLine struct{ tx *Tx }

// ReadOnLine waits for earlier Session publications before invoking read. The
// callback must not reenter the Session or block awaiting a Session operation.
func (s *Session) ReadOnLine(ctx context.Context, read func(*ReadLine) error) error {
	if read == nil {
		return reject("nil line read callback")
	}
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	state, err := s.taskSnapshot(ctx)
	if err != nil {
		return err
	}
	tx := &Tx{ctx: ctx, state: state, limits: s.limits}
	defer tx.seal()
	return read(&ReadLine{tx: tx})
}

func (r *ReadLine) check() error {
	if r == nil || r.tx == nil || r.tx.sealed.Load() {
		return ErrSealed
	}
	return r.tx.ctx.Err()
}
func (r *ReadLine) ScanEntries(q EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	if err := r.check(); err != nil {
		return Page[Entry]{}, err
	}
	return r.tx.ScanEntries(q, limit, cursor)
}
func (r *ReadLine) FindLatestHeadMarker(conversation, at ID) (Entry, bool, error) {
	if err := r.check(); err != nil {
		return Entry{}, false, err
	}
	return r.tx.LatestHeadMarker(conversation, at)
}

// LatestEntryID reads the newest visible entry, including the transaction's
// own staged writes and inherited fork history.
func (t *Tx) LatestEntryID(conversation ID) (ID, bool, error) {
	if err := t.enter(); err != nil {
		return 0, false, err
	}
	defer t.leave()
	state, err := t.current()
	if err != nil {
		return 0, false, err
	}
	entries, err := visibleHistory(state, EntryQuery{Conversation: conversation})
	if err != nil || len(entries) == 0 {
		return 0, false, err
	}
	return entries[0].ID, true, nil
}

// LatestHeadMarker reads the newest visible marker within an optional inclusive
// entry-ID ceiling. Zero selects the complete visible history.
func (t *Tx) LatestHeadMarker(conversation, at ID) (Entry, bool, error) {
	if err := t.enter(); err != nil {
		return Entry{}, false, err
	}
	defer t.leave()
	state, err := t.current()
	if err != nil {
		return Entry{}, false, err
	}
	entries, err := visibleHistory(state, EntryQuery{Conversation: conversation, MaxEntryID: at})
	if err != nil {
		return Entry{}, false, err
	}
	for _, entry := range entries {
		if entry.Head != 0 {
			owned, err := copyEntry(entry, t.limits)
			return owned, err == nil, err
		}
	}
	return Entry{}, false, nil
}
