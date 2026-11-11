package durable

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"sync"
)

// CommitPublication is the canonical stored-change envelope. Changes is a
// sealed readonly array of the upstream table/document change union.
type CommitPublication struct {
	Seq     uint64        `json:"seq"`
	Changes ReadonlyValue `json:"changes"`
}

// CommitObserver receives stored commits and one guarded ending. Callbacks must
// not block or synchronously reenter queued Session operations. Observer
// attachment and detachment are allowed from callbacks.
type CommitObserver struct {
	Advance func(context.Context, CommitPublication) error
	End     func(WatchEnd) error
}
type CommitObservation struct {
	mu       sync.Mutex
	session  *Session
	observer CommitObserver
}

func (s *Session) ObserveCommits(observer CommitObserver) (*CommitObservation, error) {
	if observer.Advance == nil || observer.End == nil {
		return nil, reject("incomplete commit observer")
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
	guard := &CommitObservation{session: s, observer: observer}
	s.commitObservers = append(s.commitObservers, guard)
	return guard, nil
}
func (h *Harness) ObserveCommits(observer CommitObserver) (*CommitObservation, error) {
	if h.closing.Load() {
		return nil, ErrClosed
	}
	return h.session.ObserveCommits(observer)
}
func (o *CommitObservation) release() (CommitObserver, *Session) {
	if o == nil {
		return CommitObserver{}, nil
	}
	o.mu.Lock()
	s, observer := o.session, o.observer
	o.session = nil
	o.observer = CommitObserver{}
	o.mu.Unlock()
	if s == nil {
		return CommitObserver{}, nil
	}
	s.observerMu.Lock()
	for i, guard := range s.commitObservers {
		if guard == o {
			copy(s.commitObservers[i:], s.commitObservers[i+1:])
			last := len(s.commitObservers) - 1
			s.commitObservers[last] = nil
			s.commitObservers = s.commitObservers[:last]
			if last == 0 {
				s.commitObservers = nil
			}
			break
		}
	}
	s.observerMu.Unlock()
	return observer, s
}
func (o *CommitObservation) Detach() { _, _ = o.release() }
func (s *Session) reportCommitObserver(ctx context.Context, err error) {
	if s.taskScheduler != nil {
		s.reportListener(ctx, err)
	} else {
		reportTaskError(s.timerReport, err)
	}
}
func (s *Session) endCommitObserver(observer CommitObserver, end WatchEnd) {
	if err := callPublicCommitObserver(func() error { return observer.End(end) }); err != nil {
		s.reportCommitObserver(context.Background(), err)
	}
}
func (o *CommitObservation) advance(ctx context.Context, prepared preparedPublicCommit) {
	o.mu.Lock()
	s, observer := o.session, o.observer
	o.mu.Unlock()
	if s == nil {
		return
	}
	err := prepared.err
	if err == nil {
		err = callPublicCommitObserver(func() error { return observer.Advance(ctx, prepared.value) })
	}
	if err != nil {
		s.reportCommitObserver(ctx, err)
		if ended, owner := o.release(); owner != nil {
			owner.endCommitObserver(ended, WatchEnd{Reason: "listener_error", Error: commitObserverEndError(err)})
		}
	}
}
func (s *Session) publicCommitObservers() []*CommitObservation {
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	return append([]*CommitObservation(nil), s.commitObservers...)
}
func (s *Session) endPublicCommitObservers() {
	cause := s.writerFailure()
	if cause == nil {
		cause = s.closeErr
	}
	if cause == nil {
		cause = errors.New("session closed")
	}
	for _, guard := range s.publicCommitObservers() {
		if observer, owner := guard.release(); owner != nil {
			owner.endCommitObserver(observer, WatchEnd{Reason: "failed", Error: cause})
		}
	}
}

type pendingPublicCommit struct {
	prepared preparedPublicCommit
	ctx      context.Context
	writer   bool
}

// Notification interest is captured at publication, but membership is selected
// later at dispatch. Own listeners may add/detach guards in between.
func (s *Session) queuePublicCommit(ctx context.Context, prepared preparedPublicCommit, writer bool) {
	s.observerMu.Lock()
	interested := len(s.commitObservers) > 0
	s.observerMu.Unlock()
	if !interested {
		return
	}
	if s.commitDispatchGate == nil {
		s.commitDispatchGate = make(chan struct{})
	}
	s.pendingPublicCommits = append(s.pendingPublicCommits, pendingPublicCommit{prepared, ctx, writer})
	s.commitDispatchNeeded.Store(true)
}
func (s *Session) drainPublicCommits(writer bool) {
	if len(s.pendingPublicCommits) == 0 {
		return
	}
	if !writer && s.pendingPublicCommits[0].writer {
		return
	}
	queued := s.pendingPublicCommits
	s.pendingPublicCommits = nil
	for _, pending := range queued {
		for _, guard := range s.publicCommitObservers() {
			guard.advance(pending.ctx, pending.prepared)
		}
	}
	clear(queued)
	if s.commitDispatchGate != nil {
		close(s.commitDispatchGate)
		s.commitDispatchGate = nil
	}
	s.commitDispatchNeeded.Store(false)
}
func (s *Session) dispatchCommitObservers() {
	if !s.commitDispatchNeeded.Load() {
		return
	}
	_ = s.line.acquire(context.Background())
	defer s.handoffLeave(nil)
	s.drainPublicCommits(true)
}

// Published task/submission continuations reacquire Session admission. Keep that
// admission ordered while releasing only Go memory to the host-dispatch phase.
func (s *Session) awaitCommitDispatch(ctx context.Context) error {
	gate := s.commitDispatchGate
	if gate == nil {
		return nil
	}
	jobHeld, outcome := s.jobHeld, s.jobOutcome
	s.jobHeld = false
	s.jobOutcome = nil
	s.line.release()
	var err error
	select {
	case <-gate:
	case <-ctx.Done():
		err = contextError(ctx)
	}
	_ = s.line.acquire(context.Background())
	s.jobHeld, s.jobOutcome = jobHeld, outcome
	return err
}

// Reporting retains the original throw slot. A listener_error ending is a
// separate Error normalisation in source guardObserver/toError, not that slot.
func callPublicCommitObserver(callback func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			// Go replaces panic(nil) with this runtime sentinel; the thrown slot
			// corresponds to source null, not a host-created Error.
			if _, nilPanic := value.(*runtime.PanicNilError); nilPanic {
				value = nil
			}
			err = newFailure(value).AsError()
		}
	}()
	return callback()
}
func commitObserverEndError(err error) error {
	var failure *Failure
	if !errors.As(err, &failure) {
		return err
	}
	switch value := failure.Value().(type) {
	case nil:
		return errors.New("null")
	case bool:
		return errors.New(strconv.FormatBool(value))
	case string:
		return errors.New(value)
	case int:
		return errors.New(strconv.Itoa(value))
	default:
		// Non-scalar host throws still need the source stringification crosswalk.
		return err
	}
}
