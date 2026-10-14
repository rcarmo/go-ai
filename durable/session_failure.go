package durable

import (
	"context"
	"errors"
)

// SessionFailed preserves the first backend cause across subsequent operations.
// It never asserts that a failed admitted write is absent after recovery.
type SessionFailed struct{ Cause error }

func (e *SessionFailed) Error() string {
	return "durable: session failed; close and reopen required: " + e.Cause.Error()
}
func (e *SessionFailed) Unwrap() error { return e.Cause }

type SessionEnd struct {
	Reason string
	Error  error
}

func (s *Session) failure() error {
	s.failureMu.Lock()
	defer s.failureMu.Unlock()
	if s.failureCause == nil {
		return nil
	}
	return &SessionFailed{Cause: s.failureCause}
}
func (s *Session) backendResult(ctx context.Context, err error, mutation bool) error {
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
		s.failureCause = err
	}
	cause := s.failureCause
	s.failureMu.Unlock()
	if !first {
		return &SessionFailed{Cause: cause}
	}
	s.closeWatches()
	// Failure fences Session admission immediately. A harness must join actual
	// retained hosts before storage closes, just like explicit Harness.Close.
	// Never join synchronously here: the failing call may own the Session line.
	if scheduler := s.taskScheduler; scheduler != nil {
		scheduler.h.closing.Store(true)
		scheduler.sealed.Store(true)
		go func() { _ = scheduler.h.Close(context.Background()) }()
	} else {
		go func() { _ = s.Close(context.Background()) }()
	}
	return &SessionFailed{Cause: cause}
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
		return SessionEnd{Reason: "failed", Error: err}, nil
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

func (g *sessionStorage) Snapshot(ctx context.Context) (Snapshot, error) {
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
func (g *sessionStorage) ScanTasks(ctx context.Context, q TaskQuery, n int, c Cursor) (Page[Task], error) {
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
	v, e := g.Storage.Entries(ctx, id, c, n)
	e = g.s.backendResult(ctx, e, false)
	return v, e
}
func (g *sessionStorage) Tasks(ctx context.Context, q Query) ([]Task, error) {
	v, e := g.Storage.Tasks(ctx, q)
	e = g.s.backendResult(ctx, e, false)
	return v, e
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
