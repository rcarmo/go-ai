package durable

import "context"

// ReadLine is a callback-scoped read capability over one committed Session
// image. Returned records are detached; escaped or overlapping calls reject.
// It exposes no storage mutation or allocator capability.
type ReadLine struct{ tx *Tx }

// ReadOnLine waits for earlier Session publications before invoking read. The
// callback must not reenter the Session or block awaiting a Session operation.
func (s *Session) ReadOnLine(ctx context.Context, read func(*ReadLine) error) (err error) {
	if read == nil {
		return reject("nil line read callback")
	}
	if err := s.enter(ctx); err != nil {
		return err
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
	}()
	// Upstream pairs `readOnLine` with `touch`.
	if err := s.touchAll(ctx); err != nil {
		return err
	}
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
	return r.tx.latestHeadMarkerAt(conversation, at)
}

// LatestEntryID reads the newest committed visible entry, including fork history.
func (t *Tx) LatestEntryID(conversation ID) (ID, bool, error) {
	if err := t.enterTableRead("latestEntryId", ALL_KEYS); err != nil {
		return 0, false, err
	}
	defer t.leave()

	entries, err := visibleHistory(t.state, EntryQuery{Conversation: conversation})
	if err != nil || len(entries) == 0 {
		return 0, false, err
	}
	return entries[0].ID, true, nil
}

// LatestHeadMarker reads the newest committed visible marker, as the public
// source Tx method does. Historical ceilings remain an internal read helper.
func (t *Tx) LatestHeadMarker(conversation ID) (Entry, bool, error) {
	return t.latestHeadMarkerAt(conversation, 0)
}

func (t *Tx) latestHeadMarkerAt(conversation, at ID) (Entry, bool, error) {
	if err := t.enterTableRead("latestHeadMarker", ALL_KEYS); err != nil {
		return Entry{}, false, err
	}
	defer t.leave()

	entries, err := visibleHistory(t.state, EntryQuery{Conversation: conversation, MaxEntryID: at})
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
