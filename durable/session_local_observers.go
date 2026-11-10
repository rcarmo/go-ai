package durable

import "context"

// Private standalone followCommits adaptation. No public ownership/scheduler
// boundary: callbacks are trusted, synchronous, read-only and must not block,
// call Storage/Session/task APIs or invoke host code. Harness waiter/abort and
// public value/reporting integration remain separately gated.
// A nil seq expresses the memory arm of the Source union (which has no seq).
type sessionObservedCommit struct {
	kind      string
	seq       *uint64
	tables    []Write
	documents []DocumentChange
}

// Immutable after attachment. Object identity represents Source function/Set
// identity without comparing Go closures or using unsafe code.
type sessionCommitListener struct {
	callback func(context.Context, sessionObservedCommit) error
	// Document observations occupy their actual position in the local Set.
	// Their guard contains host errors rather than failing the Session.
	document *sessionDocumentObservation
}

func (s *Session) followSessionCommits(listener *sessionCommitListener) (func(), error) {
	if listener == nil || listener.callback == nil {
		return nil, reject("incomplete Session-local commit listener")
	}
	if s.taskScheduler != nil {
		return nil, ErrUnsupported
	}
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	if err := s.failure(); err != nil {
		return nil, err
	}
	if s.closing.Load() {
		return nil, ErrClosed
	}
	found := false
	for _, existing := range s.localCommitListeners {
		found = found || existing == listener
	}
	if !found {
		if s.observerProcessCountLocked() >= s.observerCapacity() {
			return nil, reject("observer process count limit")
		}
		s.localCommitListeners = append(s.localCommitListeners, listener)
	}
	// Source returns () => listeners.delete(listener): an old detacher also
	// deletes a later re-add of that identical function. Like Source, this
	// caller-owned closure retains its Session/listener until the caller drops
	// it; it is deliberately not the guarded host observation's spent handle.
	return func() {
		s.observerMu.Lock()
		defer s.observerMu.Unlock()
		for i, existing := range s.localCommitListeners {
			if existing == listener {
				copy(s.localCommitListeners[i:], s.localCommitListeners[i+1:])
				last := len(s.localCommitListeners) - 1
				s.localCommitListeners[last] = nil
				s.localCommitListeners = s.localCommitListeners[:last]
				if last == 0 {
					s.localCommitListeners = nil
				}
				break
			}
		}
	}, nil
}

// Own listeners iterate a copied Set, not guarded host handles. Deleting a
// later member during a callback changes the NEXT publication, not this one.
// Collect the first fault while still bringing every copied peer up to date.
// The caller publishes queued host work, THEN fails the Session; the current
// already-published commit still resolves (writer.ts listener failure arm).
func (s *Session) publishLocalCommit(ctx context.Context, kind string, publication CommitPublication) error {
	s.observerMu.Lock()
	listeners := append([]*sessionCommitListener(nil), s.localCommitListeners...)
	s.observerMu.Unlock()
	if len(listeners) == 0 {
		return nil
	}
	event := sessionObservedCommit{kind: kind, documents: publication.Documents}
	if kind == "stored" {
		seq := publication.Seq
		event.seq, event.tables = &seq, publication.Tables
	}
	ctx = context.WithoutCancel(ctx)
	var first error
	readonlyValues := make([]ReadonlyValue, len(publication.Documents))
	for _, listener := range listeners {
		if listener.document != nil {
			listener.document.advance(ctx, publication, readonlyValues)
			continue
		}
		if err := callSessionCommitObserver(func() error { return listener.callback(ctx, event) }); first == nil {
			first = err
		}
	}
	return first
}

func (s *Session) clearLocalCommitListeners() {
	s.observerMu.Lock()
	s.localCommitListeners = nil
	s.observerMu.Unlock()
}
