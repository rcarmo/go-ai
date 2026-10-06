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
	after          func(time.Duration) <-chan time.Time
}

func newToolProgress(write func() (int, error), report func(error)) *toolProgress {
	return newToolProgressClock(write, report, time.Now, time.After)
}
func newToolProgressClock(write func() (int, error), report func(error), now func() time.Time, after func(time.Duration) <-chan time.Time) *toolProgress {
	p := &toolProgress{wake: make(chan struct{}, 1), halt: make(chan struct{}), done: make(chan struct{}), write: write, report: report, now: now, after: after}
	go p.run()
	return p
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
func progressDelay(bytes int) time.Duration {
	// ceil byte-proportional duration; negative byte reports buy only the minimum.
	if bytes <= 10*1024 {
		return 100 * time.Millisecond
	}
	return time.Duration(bytes) * time.Second / (100 * 1024)
}
func (p *toolProgress) run() {
	defer close(p.done)
	var next time.Time
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
			if delay := next.Sub(p.now()); delay > 0 {
				select {
				case <-p.halt:
					return
				case <-p.after(delay):
				}
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
			next = started.Add(progressDelay(bytes))
			if err != nil {
				next = started.Add(100 * time.Millisecond)
			}
			settleProgress(waiters, err)
			if err != nil && p.report != nil {
				p.report(err)
			}
		}
	}
}
