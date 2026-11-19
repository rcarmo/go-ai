package durable

import "context"

// ReadLine is a callback-scoped read capability over committed Session state.
// Each read waits for earlier publications that affect it. Returned records
// are detached; escaped or overlapping calls reject.
type ReadLine struct{ tx *Tx }

// ReadOnLine runs read on the Session line. The callback must not reenter the
// Session or block awaiting a Session operation.
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
	state, err := s.taskSnapshot(ctx)
	if err != nil {
		return err
	}
	tx := &Tx{ctx: ctx, state: state, limits: s.limits, session: s, keyedReads: s.hasNativeReadImage()}
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
	if storage := r.tx.delegatedHistoryStorage(); storage != nil {
		if err := r.tx.enterTableRead("findLatestHeadMarker", conversationKey(conversation), appendKey(conversation)); err != nil {
			return Entry{}, false, err
		}
		defer r.tx.leave()
		return storage.FindLatestHeadMarker(r.tx.ctx, conversation, at)
	}
	return r.tx.latestHeadMarkerAt(conversation, at)
}

// Keep the adopted image for validation; uncached history reads on a public
// backend still use the backend's public methods after crossing the line gate.
func (t *Tx) delegatedHistoryStorage() *sessionStorage {
	if t.session == nil || !t.session.delegatedStorage {
		return nil
	}
	return t.session.guardedStorage()
}

// LatestEntryID reads the newest committed visible entry, including fork history.
func (t *Tx) LatestEntryID(conversation ID) (ID, bool, error) {
	if err := t.enterTableRead("latestEntryId", conversationKey(conversation), appendKey(conversation)); err != nil {
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
	if err := t.enterTableRead("latestHeadMarker", conversationKey(conversation), appendKey(conversation)); err != nil {
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
