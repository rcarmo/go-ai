package durable

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	contextExpiryTimer = iota
	documentExpiryTimer
)

type sessionTimerChange struct{ stop, arm *sessionTimer }
type sessionTimer struct {
	session          *Session
	slot             int
	delay            time.Duration
	callback         func()
	started, stopped atomic.Bool
	mu               sync.Mutex
	cancel           func()
}

// Called on the Session line. Only the last requested arm for each of the two
// cache domains is retained, regardless of the number of reads in a transaction.
func (s *Session) newSessionTimer(slot int, delay time.Duration, callback func()) *sessionTimer {
	node := &sessionTimer{session: s, slot: slot, delay: delay, callback: callback}
	s.pendingTimers[slot].arm = node
	return node
}

// Stop invalidates callbacks immediately but defers the host cancellation until
// leave. An unstarted replacement never needs a host call, so repeated on-line
// reschedules occupy two fixed slots, not an unbounded action queue.
func (t *sessionTimer) Stop() {
	if t.stopped.Swap(true) {
		return
	}
	change := &t.session.pendingTimers[t.slot]
	if change.arm == t {
		change.arm = nil
	}
	if t.started.Load() {
		change.stop = t
	}
}
func (t *sessionTimer) disarm() {
	t.mu.Lock()
	cancel := t.cancel
	t.cancel = nil
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (t *sessionTimer) arm() {
	if t.stopped.Load() {
		return
	}
	s := t.session
	cancel := armHostTimer(s.setTimer, s.timerReport, &s.timerFallbackReported, "SessionOptions.SetTimer", func() {
		if !t.stopped.Load() && !s.closing.Load() {
			t.callback()
		}
	}, t.delay, TimerOptions{Background: true})
	t.mu.Lock()
	stopped := t.stopped.Load()
	if !stopped {
		t.cancel = cancel
	}
	t.mu.Unlock()
	if stopped && cancel != nil {
		cancel()
	}
}
