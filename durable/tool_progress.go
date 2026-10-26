package durable

import (
	"sync"
	"time"
)

type progressWaiter chan error

// toolProgress is the native port of pinned Progress: writes are serial and
// dirty marks during a write/delay coalesce. Stop joins the actual write and
// returns waiters whose update belongs to the terminal flush.
type toolProgress struct {
	mu             sync.Mutex
	dirty, stopped bool
	waiters        []progressWaiter
	wake           chan struct{}
	halt           chan struct{}
	done           chan struct{}
	write          func() (int, error)
	report         func(error)
	now            func() time.Time
	arm            func(time.Duration) (<-chan time.Time, func(), error)
	interval       time.Duration
}

func newToolProgress(write func() (int, error), report func(error)) *toolProgress {
	return newToolProgressTimer(write, report, time.Now, func(delay time.Duration) (<-chan time.Time, func(), error) {
		timer := time.NewTimer(delay)
		return timer.C, func() { timer.Stop() }, nil
	}, 100*time.Millisecond)
}
func newToolProgressClock(write func() (int, error), report func(error), now func() time.Time, after func(time.Duration) <-chan time.Time) *toolProgress {
	return newToolProgressInterval(write, report, now, after, 100*time.Millisecond)
}
func newToolProgressInterval(write func() (int, error), report func(error), now func() time.Time, after func(time.Duration) <-chan time.Time, interval time.Duration) *toolProgress {
	return newToolProgressTimer(write, report, now, func(delay time.Duration) (<-chan time.Time, func(), error) {
		return after(delay), func() {}, nil
	}, interval)
}
func newToolProgressTimer(write func() (int, error), report func(error), now func() time.Time, arm func(time.Duration) (<-chan time.Time, func(), error), interval time.Duration) *toolProgress {
	p := &toolProgress{interval: interval, wake: make(chan struct{}, 1), halt: make(chan struct{}), done: make(chan struct{}), write: write, report: report, now: now, arm: arm}
	go p.run()
	return p
}
func newRuntimeToolProgress(write func() (int, error), report func(error), runtime *TaskRuntime, interval time.Duration) *toolProgress {
	now, arm := runtimeProgressTiming(runtime)
	return newToolProgressTimer(write, report, now, arm, interval)
}
func runtimeProgressTiming(runtime *TaskRuntime) (func() time.Time, func(time.Duration) (<-chan time.Time, func(), error)) {
	var last time.Time // only the progress worker reads the clock
	return func() time.Time {
			if ms, err := runtime.Now(); err == nil {
				last = time.UnixMilli(ms)
			}
			return last
		}, func(delay time.Duration) (<-chan time.Time, func(), error) {
			ticks := make(chan time.Time, 1)
			cancel, err := runtime.SetTimer(func() {
				select {
				case ticks <- time.Time{}:
				default:
				}
			}, delay, TimerOptions{})
			return ticks, cancel, err
		}
}
func (p *toolProgress) mark(wait bool) progressWaiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	var waiter progressWaiter
	if wait {
		waiter = make(progressWaiter, 1)
		p.waiters = append(p.waiters, waiter)
	}
	p.dirty = true
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return waiter
}
func (p *toolProgress) stop() []progressWaiter {
	p.mu.Lock()
	if !p.stopped {
		p.stopped = true
		close(p.halt)
	}
	p.mu.Unlock()
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	pending := p.waiters
	p.waiters = nil
	return pending
}
func settleProgress(waiters []progressWaiter, err error) {
	for _, waiter := range waiters {
		waiter <- err
		close(waiter)
	}
}
func progressDelayInterval(bytes int, minimum time.Duration) time.Duration {
	if bytes <= 0 {
		return minimum
	}
	proportional := time.Duration(bytes) * time.Second / (100 * 1024)
	if proportional < minimum {
		return minimum
	}
	return proportional
}
func progressInterval(ms int64) time.Duration {
	if ms > int64((1<<63-1)/int64(time.Millisecond)) {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(ms) * time.Millisecond
}
func (p *toolProgress) run() {
	defer close(p.done)
	var next time.Time
	paced := false
	for {
		select {
		case <-p.halt:
			return
		case <-p.wake:
		}
		for {
			p.mu.Lock()
			dirty, stopped := p.dirty, p.stopped
			p.mu.Unlock()
			if stopped {
				return
			}
			if !dirty {
				break
			}
			armFailed := false
			for paced {
				delay := next.Sub(p.now())
				if delay <= 0 {
					break
				}
				ticks, disarm, err := p.arm(delay)
				if err != nil {
					p.mu.Lock()
					waiters := p.waiters
					p.waiters = nil
					p.dirty = false
					p.mu.Unlock()
					settleProgress(waiters, err)
					if p.report != nil {
						p.report(err)
					}
					armFailed = true
					break
				}
				select {
				case <-p.halt:
					disarm()
					return
				case <-ticks:
					disarm()
				}
				// A host timer may fire early. Recheck the same clock rather
				// than flushing before the size/minimum spacing has elapsed.
			}
			if armFailed {
				break
			}
			p.mu.Lock()
			if p.stopped {
				p.mu.Unlock()
				return
			}
			p.dirty = false
			waiters := p.waiters
			p.waiters = nil
			p.mu.Unlock()
			started := p.now()
			bytes, err := p.write()
			paced = true
			next = started.Add(progressDelayInterval(bytes, p.interval))
			if err != nil {
				next = started.Add(p.interval)
			}
			settleProgress(waiters, err)
			if err != nil && p.report != nil {
				p.report(err)
			}
		}
	}
}
