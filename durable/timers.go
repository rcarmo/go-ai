package durable

import (
	"fmt"
	"sync"
	"time"
)

// TimerOptions distinguishes optional memory cleanup from work that must wake.
type TimerOptions struct{ Background bool }

// SetTimer must return promptly; nil declines. Its cancellation must not block.
// Callbacks must not block and may fire at most once. The runtime fences late fires.
type SetTimer func(callback func(), delay time.Duration, options TimerOptions) (cancel func())

func (h *Harness) armTimer(callback func(), delay time.Duration, options TimerOptions) func() {
	if delay < 0 {
		delay = 0
	}
	var cancel func()
	var failure error
	if h.options.SetTimer != nil {
		func() {
			defer func() {
				if value := recover(); value != nil {
					failure = fmt.Errorf("timer host panic: %v", value)
				}
			}()
			cancel = h.options.SetTimer(callback, delay, options)
		}()
		if cancel == nil && failure == nil && !options.Background {
			failure = fmt.Errorf("HarnessOptions.SetTimer declined a timer that paces work; using time.AfterFunc")
		}
	} else {
		timer := time.AfterFunc(delay, callback)
		cancel = func() { timer.Stop() }
	}
	if failure != nil {
		if options.Background || h.timerFallbackReported.CompareAndSwap(false, true) {
			reportTaskError(h.options.OnReport, failure)
		}
	}
	if cancel == nil && !options.Background {
		timer := time.AfterFunc(delay, callback)
		cancel = func() { timer.Stop() }
	}
	if cancel == nil {
		return nil
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			defer func() {
				if value := recover(); value != nil {
					reportTaskError(h.options.OnReport, fmt.Errorf("timer cancellation panic: %v", value))
				}
			}()
			cancel()
		})
	}
}

type invocationTimer struct {
	cancel func()
	closed bool
}

// SetTimer is invocation-scoped, non-durable pacing, not a continuation.
// It disarms at invocation end and suppresses late host fires. Timer admission
// is bounded by MaxPage; cancellation and host callbacks run off the Session line.
func (r *TaskRuntime) SetTimer(callback func(), delay time.Duration, options ...TimerOptions) (func(), error) {
	if err := r.timerAvailable(); err != nil {
		return nil, err
	}
	if callback == nil || len(options) > 1 {
		return nil, reject("invalid invocation timer")
	}
	var mode TimerOptions
	if len(options) == 1 {
		mode = options[0]
	}
	node := &invocationTimer{}
	r.timerMu.Lock()
	if err := r.timerAvailable(); err != nil {
		r.timerMu.Unlock()
		return nil, err
	}
	if len(r.timers) >= r.harness.session.limits.MaxPage {
		r.timerMu.Unlock()
		return nil, reject("invocation timer capacity")
	}
	if r.timers == nil {
		r.timers = map[*invocationTimer]struct{}{}
	}
	r.timers[node] = struct{}{}
	r.timerMu.Unlock()
	disarm := func() {
		r.timerMu.Lock()
		node.closed = true
		delete(r.timers, node)
		cancel := node.cancel
		node.cancel = nil
		r.timerMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	fire := func() {
		r.timerMu.Lock()
		if node.closed {
			r.timerMu.Unlock()
			return
		}
		node.closed = true
		delete(r.timers, node)
		node.cancel = nil
		r.timerMu.Unlock()
		if r.timerAvailable() != nil {
			return
		}
		defer func() {
			if value := recover(); value != nil {
				r.harness.scheduler.report(fmt.Errorf("invocation timer callback panic: %v", value))
			}
		}()
		callback()
	}
	cancel := r.harness.armTimer(fire, delay, mode)
	r.timerMu.Lock()
	ended := r.timerAvailable() != nil
	stopped := node.closed || ended || cancel == nil
	if stopped {
		node.closed = true
		delete(r.timers, node)
	} else {
		node.cancel = cancel
	}
	r.timerMu.Unlock()
	if stopped && cancel != nil {
		cancel()
	}
	if ended {
		return nil, ErrSealed
	}
	return disarm, nil
}

func (r *TaskRuntime) stopTimers() {
	r.timerMu.Lock()
	cancellations := make([]func(), 0, len(r.timers))
	for node := range r.timers {
		node.closed = true
		if node.cancel != nil {
			cancellations = append(cancellations, node.cancel)
		}
		node.cancel = nil
	}
	r.timers = nil
	r.timerMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (r *TaskRuntime) timerAvailable() error {
	if err := r.check(); err != nil {
		return err
	}
	if r.marked.Load() && !r.abortMode {
		return ErrSealed
	}
	return nil
}
