package durable

import (
	"context"
	"errors"
	"sync"
)

// Private stored-commit lane, not a replacement for SubscribeCommits. As in
// the pinned observeCommits contract, callbacks are synchronous, must not block
// or call Session APIs, and receive a read-only publication. This trusted
// internal boundary also works with a Harness scheduler but does not expose
// mutable maps as a public Go API. Source microtask dispatch, public ownership
// and Harness reporting remain separate work. report is a fast private hook,
// not the legacy/Harness reporter (whose off-line reentry is unchanged).
type sessionCommitObserver struct {
	advance func(context.Context, CommitPublication) error
	end     func(WatchEnd) error
	report  func(context.Context, error, ReportInfo)
}

type sessionCommitObservation struct {
	mu       sync.Mutex
	source   *Session
	observer sessionCommitObserver
}

func (s *Session) observeStoredCommits(observer sessionCommitObserver) (*sessionCommitObservation, error) {
	if observer.advance == nil || observer.end == nil {
		return nil, reject("incomplete synchronous commit observer")
	}
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	if err := s.failure(); err != nil {
		return nil, err
	}
	if s.closing.Load() {
		return nil, ErrClosed
	}
	if s.observerProcessCountLocked() >= s.observerCapacity() {
		return nil, reject("observer process count limit")
	}
	observation := &sessionCommitObservation{source: s, observer: observer}
	s.syncCommitObservers = append(s.syncCommitObservers, observation)
	return observation, nil
}

func (s *Session) observerProcessCountLocked() int {
	return len(s.watches) + len(s.subscriptions) + len(s.syncCommitObservers) + len(s.localCommitListeners)
}

// Release before end, and retain no callback/report/source from a spent handle.
// Never call user code or acquire the source lock while holding the guard lock.
func (o *sessionCommitObservation) release() (sessionCommitObserver, bool) {
	o.mu.Lock()
	s, observer := o.source, o.observer
	o.source, o.observer = nil, sessionCommitObserver{}
	o.mu.Unlock()
	if s == nil {
		return sessionCommitObserver{}, false
	}
	s.observerMu.Lock()
	for i, candidate := range s.syncCommitObservers {
		if candidate == o {
			copy(s.syncCommitObservers[i:], s.syncCommitObservers[i+1:])
			last := len(s.syncCommitObservers) - 1
			s.syncCommitObservers[last] = nil
			s.syncCommitObservers = s.syncCommitObservers[:last]
			if last == 0 {
				s.syncCommitObservers = nil
			}
			break
		}
	}
	s.observerMu.Unlock()
	return observer, true
}
func (o *sessionCommitObservation) detach() { _, _ = o.release() }

func callSessionCommitObserver(callback func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			if cause, ok := value.(error); ok {
				err = cause
			} else {
				err = errors.New("synchronous commit observer panicked")
			}
		}
	}()
	return callback()
}
func reportSessionCommitObserver(observer sessionCommitObserver, ctx context.Context, err error) {
	if observer.report != nil {
		_ = callSessionCommitObserver(func() error {
			observer.report(ctx, err, ReportInfo{Type: "listener_error"})
			return nil
		})
	}
}
func endSessionCommitObserver(observer sessionCommitObserver, end WatchEnd) {
	if err := callSessionCommitObserver(func() error { return observer.end(end) }); err != nil {
		reportSessionCommitObserver(observer, context.Background(), err)
	}
}
func (o *sessionCommitObservation) advance(ctx context.Context, publication CommitPublication) {
	o.mu.Lock()
	observer, attached := o.observer, o.source != nil
	o.mu.Unlock()
	if !attached {
		return
	}
	if err := callSessionCommitObserver(func() error { return observer.advance(ctx, publication) }); err != nil {
		// Source guard order: report, detach, end unless already detached while
		// reporting. Reporter/end faults cannot escape into durable adoption.
		reportSessionCommitObserver(observer, ctx, err)
		if _, released := o.release(); released {
			endSessionCommitObserver(observer, WatchEnd{Reason: "listener_error", Error: err})
		}
	}
}
func (s *Session) storedCommitObservers() []*sessionCommitObservation {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	return append([]*sessionCommitObservation(nil), s.syncCommitObservers...)
}

// Selection happens at publication, not preparation. Guards recheck detachment
// at callback admission; a prior observer can detach a later captured guard.
func (s *Session) publishStoredCommit(ctx context.Context, publication CommitPublication) {
	for _, observer := range s.storedCommitObservers() {
		observer.advance(context.WithoutCancel(ctx), publication)
	}
}
func (s *Session) endStoredCommitObservers() {
	s.failureMu.Lock()
	cause := s.failureCause
	s.failureMu.Unlock()
	if cause == nil {
		cause = s.closeErr
	}
	if cause == nil {
		cause = errors.New("session closed")
	}
	end := WatchEnd{Reason: "failed", Error: cause}
	for _, observation := range s.storedCommitObservers() {
		if observer, released := observation.release(); released {
			endSessionCommitObserver(observer, end)
		}
	}
}
