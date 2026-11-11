package durable

import (
	"context"
	"errors"
)

// Failure carries a reported failure value, including storage failures and
// guarded public commit-observer panics. The original models
// the slot as `{ error: unknown }`, so the value itself is observable and presence
// is not the same question: a present null, undefined or falsy value is still a
// failure. A nil *Failure therefore means "no failure".
type Failure struct{ value any }

func newFailure(value any) *Failure { return &Failure{value: value} }

// asFailure classifies an incoming backend error. A value-carrying *Failure is
// kept exactly as reported; any other error becomes a Failure whose value is that
// error, so its identity survives for errors.Is/errors.As classification.
func asFailure(err error) *Failure {
	var existing *Failure
	if errors.As(err, &existing) {
		return existing
	}
	return newFailure(err)
}

// Value returns the failure value exactly as the backend reported it.
func (f *Failure) Value() any {
	if f == nil {
		return nil
	}
	return f.value
}

// AsError gives the failure a Go error view without losing the original value:
// an error value is returned as itself, and any other value is represented by
// this *Failure, which is never nil, so a present failure cannot read as absent.
func (f *Failure) AsError() error {
	if f == nil {
		return nil
	}
	if err, ok := f.value.(error); ok && err != nil {
		return err
	}
	return f
}

func (f *Failure) Error() string {
	if err, ok := f.value.(error); ok && err != nil {
		return err.Error()
	}
	return "durable: storage failure"
}

func (f *Failure) Unwrap() error {
	if err, ok := f.value.(error); ok {
		return err
	}
	return nil
}

// SessionFailed preserves the first backend cause across subsequent operations.
// It never asserts that a failed admitted write is absent after recovery.
// Failure returns the cause exactly as the backend reported it; Cause is the Go
// error view kept for existing classification.
type SessionFailed struct {
	Cause   error
	failure *Failure
}

// Failure returns the original failure value behind this cause.
func (e *SessionFailed) Failure() *Failure {
	if e == nil {
		return nil
	}
	if e.failure != nil {
		return e.failure
	}
	return asFailure(e.Cause)
}

func (e *SessionFailed) Error() string {
	if e.Cause == nil {
		return "durable: session failed; close and reopen required"
	}
	return "durable: session failed; close and reopen required: " + e.Cause.Error()
}
func (e *SessionFailed) Unwrap() error { return e.Cause }

// SessionEnd reports how a Session ended. Failure carries the value the Session
// failed with, exactly as the backend reported it; nil means the Session did not
// fail. Error keeps the Go error view for existing classification.
type SessionEnd struct {
	Reason  string
	Error   error
	Failure *Failure
}

func (s *Session) failure() error {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	if s.failureCause == nil {
		return nil
	}
	return &SessionFailed{Cause: s.failureCause, failure: s.failureValue}
}
func (s *Session) backendResult(ctx context.Context, err error, mutation bool) error {
	return s.backendResultAt(ctx, err, mutation, nil, nil)
}

func (s *Session) backendResultAt(ctx context.Context, err error, mutation bool, at *uint64, own []uint64) error {
	if failure := s.failure(); failure != nil {
		return failure
	}
	if err == nil {
		return s.failure()
	}
	if !mutation {
		var request *StorageRequestError
		var rejected *StorageRejected
		if errors.As(err, &request) {
			return err
		}
		if errors.As(err, &rejected) {
			return &StorageRequestError{Reason: rejected.Reason}
		}
		if ctx != nil && ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			return err
		}
	}
	s.failureMu.Lock()
	first := s.failureCause == nil
	if first {
		// The value view and the Go error view answer different questions. The
		// cause chain must stay exactly as the backend reported it so classification
		// (poison, rejection, cancellation, injected identity) still works, while the
		// value is what the original would have thrown.
		s.failureValue = asFailure(err)
		s.failureCause = err
		s.failureAt = s.lastDelivered
		if at != nil {
			s.failureAt = *at
		} else if s.jobOutcome != nil {
			s.failureAt = s.jobOutcome.index
		}
		s.failureOwn = make(map[uint64]bool, len(own))
		for _, index := range own {
			s.failureOwn[index] = true
		}
	}
	cause := s.failureCause
	value := s.failureValue
	s.failureMu.Unlock()
	if !first {
		return &SessionFailed{Cause: cause, failure: value}
	}
	s.closeWatches()
	// Failure fences Session admission immediately. A harness must join actual
	// retained hosts before storage closes, just like explicit Harness.Close.
	// Never join synchronously here: the failing call may own the Session line.
	if scheduler := s.taskScheduler; scheduler != nil {
		scheduler.h.closing.Store(true)
		scheduler.sealed.Store(true)
		// Notification is caller-owned: it may reenter Close or wait for Closed.
		// Do not let a blocked reporter delay cancellation or storage settlement.
		go func() {
			// Failure fences immediately, but reporting can reenter native
			// bookkeeping. Let the writer discard provisional authority before
			// exposing the failed job to a reporter that may inspect it.
			if drained := s.writer.drained(); drained != nil {
				<-drained
			}
			scheduler.h.report(ctx, cause, ReportInfo{Type: "session_failed"})
		}()
		go func() { _ = scheduler.h.Close(context.Background()) }()
	} else {
		go func() { _ = s.Close(context.Background()) }()
	}
	// GuardedStorage.#call latches the first backend failure, then rethrows
	// that call's own error. Only calls already failed by someone else receive
	// SessionFailed. Delivery-boundary poisoning is handled separately.
	return err
}

// Closed waits for normal or failure-driven shutdown; caller cancellation only
// ends this waiter, never storage settlement.
func (s *Session) Closed(ctx context.Context) (SessionEnd, error) {
	if ctx == nil {
		return SessionEnd{}, reject("nil close wait context")
	}
	select {
	case <-ctx.Done():
		return SessionEnd{}, ctx.Err()
	case <-s.done:
	}
	if err := s.failure(); err != nil {
		var failed *SessionFailed
		if errors.As(err, &failed) {
			return SessionEnd{Reason: "failed", Error: err, Failure: failed.Failure()}, nil
		}
		return SessionEnd{Reason: "failed", Error: err, Failure: asFailure(err)}, nil
	}
	if s.closeErr != nil {
		return SessionEnd{Reason: "failed", Error: s.closeErr}, nil
	}
	return SessionEnd{Reason: "closed"}, nil
}

// Build the read guard at the call site: retain the original backend identity
// and honour adapters installed on the Session line after Open.
func (s *Session) guardedStorage() *sessionStorage { return &sessionStorage{Storage: s.store, s: s} }

type sessionStorage struct {
	Storage
	s *Session
}

// Snapshot callers hold the Session line. Native projections remain detached;
// adapters retain their complete data/error contract through the original call.
func (g *sessionStorage) Snapshot(ctx context.Context) (Snapshot, error) {
	if g.s.hasNativeReadImage() {
		image, err := g.s.taskSnapshot(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		return cloneState(image, g.s.core.limits)
	}
	v, e := g.Storage.Snapshot(ctx)
	e = g.s.backendResult(ctx, e, false)
	if e != nil {
		return Snapshot{}, e
	}
	return v, nil
}
func (g *sessionStorage) Limits() (Limits, error) {
	v, e := g.Storage.Limits()
	e = g.s.backendResult(context.Background(), e, false)
	return v, e
}
func (g *sessionStorage) ScanConversations(ctx context.Context, q ConversationQuery, n int, c Cursor) (Page[Conversation], error) {
	v, e := g.Storage.ScanConversations(ctx, q, n, c)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) ScanEntries(ctx context.Context, q EntryQuery, n int, c Cursor) (Page[Entry], error) {
	v, e := g.Storage.ScanEntries(ctx, q, n, c)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) ScanTasks(ctx context.Context, q TaskQuery, n int, c Cursor) (Page[TaskRecord], error) {
	v, e := g.Storage.ScanTasks(ctx, q, n, c)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) ScanSubmissions(ctx context.Context, q SubmissionQuery, n int, c Cursor) (Page[Submission], error) {
	v, e := g.Storage.ScanSubmissions(ctx, q, n, c)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) Conversations(ctx context.Context, q Query) ([]Conversation, error) {
	v, e := g.Storage.Conversations(ctx, q)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) Entries(ctx context.Context, id ID, c EntryCursor, n int) ([]Entry, error) {
	if g.s.hasNativeReadImage() {
		image, err := g.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		return legacyEntries(image, id, c, n, g.s.core.limits)
	}
	v, e := g.Storage.Entries(ctx, id, c, n)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	v, ok, e := g.Storage.Task(ctx, id)
	e = g.s.backendResult(ctx, e, false)
	return v, ok, e
}
func (g *sessionStorage) Submissions(ctx context.Context, q Query) ([]Submission, error) {
	v, e := g.Storage.Submissions(ctx, q)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) Documents(ctx context.Context, q Query) ([]Document, error) {
	v, e := g.Storage.Documents(ctx, q)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) Request(ctx context.Context, id ID, key string) (Submission, bool, error) {
	v, ok, e := g.Storage.Request(ctx, id, key)
	e = g.s.backendResult(ctx, e, false)
	return v, ok, e
}

// A failure-driven shutdown retains its first cause at Harness boundaries too.
// Closing is a lifecycle flag, never a replacement for a known Session failure.
func (h *Harness) closedError() error {
	if failure := h.session.failure(); failure != nil {
		return failure
	}
	return ErrClosed
}

// deliveredError ports #poisoned: later undelivered jobs inherit the first cause,
// except parts whose outcome belongs to the rejected storage call itself.
func (s *Session) deliveredError(outcome *sessionOutcome, err error) error {
	if outcome == nil {
		return err
	}
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	if s.failureCause != nil && outcome.index > s.failureAt && !s.failureOwn[outcome.index] {
		return &SessionFailed{Cause: s.failureCause, failure: s.failureValue}
	}
	return err
}
