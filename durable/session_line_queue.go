package durable

import (
	"context"
	"sync"
)

// sessionLine is the Session's line as an admission-ordered queue, matching the
// `#tail` promise chain of pinned v1.5.0 src/session/session.ts: jobs run one at a
// time, and the token passes to whoever has been waiting longest.
//
// The previous implementation was a one-slot buffered channel. That was a deviation
// with two visible consequences:
//   - a released token is contended, so a job that had given the line up could be
//     overtaken by a job admitted later, which the chain does not allow; and
//   - publication, host joins and close all raced for that one slot, so a
//     failure-driven close could get ahead of the very publication that had to
//     settle the part it was waiting on.
//
// Fairness is the substance here. Upstream's ordering guarantee is about admission
// order, and a contended slot provides none of it. This version hands the token
// directly to the head waiter, so the queue cannot be jumped.
type sessionLine struct {
	mu      sync.Mutex
	free    bool
	waiters []*lineWaiter
	// changed is closed and replaced on every queue mutation, so an observer can
	// wait for a state it cannot see rather than spinning on one it can. Tests use
	// it; spinning on pending() burned CPU in a workspace that had already OOMed.
	changed chan struct{}
}

// waitPending blocks until at least n jobs are queued.
func (l *sessionLine) waitPending(n int) {
	for {
		l.mu.Lock()
		count := len(l.waiters)
		signal := l.changed
		l.mu.Unlock()
		if count >= n {
			return
		}
		<-signal
	}
}

// broadcastLocked replaces the change signal. Callers must hold l.mu.
func (l *sessionLine) broadcastLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}

type lineWaiter struct {
	ready   chan struct{}
	granted bool // the token was handed to this waiter; it owns the line
}

func newSessionLine() *sessionLine {
	return &sessionLine{free: true, changed: make(chan struct{})}
}

// acquire takes the line in arrival order. A canceled caller never holds it, and a
// caller canceled after being handed the token returns it rather than stranding it.
func (l *sessionLine) acquire(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	// Consult cancellation at every queue admission, including the fast path.
	// Context observers must not depend on whether another job happened to
	// hold the queue at this instant.
	done := ctx.Done()
	l.mu.Lock()
	// Only a caller arriving to an empty queue may take the line at once: a handoff
	// in flight already promised it to the head of the queue.
	if l.free && len(l.waiters) == 0 {
		l.free = false
		l.mu.Unlock()
		return nil
	}
	w := &lineWaiter{ready: make(chan struct{})}
	l.waiters = append(l.waiters, w)
	l.broadcastLocked()
	l.mu.Unlock()

	select {
	case <-done:
		l.mu.Lock()
		if w.granted {
			// Handed the token while canceling. Own it long enough to return it.
			l.mu.Unlock()
			l.release()
			return contextError(ctx)
		}
		l.removeLocked(w)
		l.broadcastLocked()
		l.mu.Unlock()
		return contextError(ctx)
	case <-w.ready:
		return nil
	}
}

// release hands the line to the longest-waiting job, or frees it if none is waiting.
func (l *sessionLine) release() {
	l.mu.Lock()
	if len(l.waiters) > 0 {
		w := l.waiters[0]
		l.waiters = l.waiters[1:]
		w.granted = true
		l.broadcastLocked()
		l.mu.Unlock()
		close(w.ready)
		return
	}
	l.free = true
	l.broadcastLocked()
	l.mu.Unlock()
}

func (l *sessionLine) removeLocked(w *lineWaiter) {
	for i, candidate := range l.waiters {
		if candidate == w {
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			return
		}
	}
}

// pending reports queued jobs, for assertions and diagnostics.
func (l *sessionLine) pending() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.waiters)
}

// held reports whether some job currently owns the line. A grant in flight counts
// as held, since the token has already left the queue and been promised.
func (l *sessionLine) held() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return !l.free || len(l.waiters) > 0
}

// Source attachment cancellation retains an Error-valued AbortSignal.reason.
// Consult Err at the same admission boundary, then retain Go's specific cause
// instead of replacing WithCancelCause's error with the generic sentinel.
func contextError(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return err
}
