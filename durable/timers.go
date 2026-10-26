package durable

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TimerOptions distinguishes optional memory cleanup from work that must wake.
type TimerOptions struct{ Background bool }

// SetTimer must return promptly; nil declines. Its cancellation must not block.
// Callbacks must not block and may fire at most once. The runtime fences late fires.
type SetTimer func(callback func(), delay time.Duration, options TimerOptions) (cancel func())

func (h *Harness) armTimer(callback func(), delay time.Duration, options TimerOptions) func() {
	return armHostTimer(h.options.SetTimer, func(err error) {
		h.report(context.Background(), err, ReportInfo{Type: "host_error", Callback: "setTimer"})
	}, &h.timerFallbackReported, "HarnessOptions.SetTimer", callback, delay, options)
}
func armHostTimer(setTimer SetTimer, report func(error), fallbackReported *atomic.Bool, label string, callback func(), delay time.Duration, options TimerOptions) func() {
	if delay < 0 {
		delay = 0
	}
	var cancel func()
	var failure error
	if setTimer != nil {
		func() {
			defer func() {
				if value := recover(); value != nil {
					failure = fmt.Errorf("timer host panic: %v", value)
				}
			}()
			cancel = setTimer(callback, delay, options)
		}()
		if cancel == nil && failure == nil && !options.Background {
			failure = fmt.Errorf("%s declined a timer that paces work; using time.AfterFunc", label)
		}
	} else {
		timer := time.AfterFunc(delay, callback)
		cancel = func() { timer.Stop() }
	}
	if failure != nil {
		if options.Background || fallbackReported.CompareAndSwap(false, true) {
			reportTaskError(report, failure)
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
					reportTaskError(report, fmt.Errorf("timer cancellation panic: %v", value))
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
				r.report(fmt.Errorf("invocation timer callback panic: %v", value), r.reportInfo("task_error", ""))
			}
		}()
		callback()
	}
	cancel := armHostTimer(r.harness.options.SetTimer, func(err error) {
		info := r.reportInfo("host_error", "")
		info.Extension = ""
		info.Callback = "setTimer"
		r.report(err, info)
	}, &r.harness.timerFallbackReported, "HarnessOptions.SetTimer", fire, delay, mode)
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
