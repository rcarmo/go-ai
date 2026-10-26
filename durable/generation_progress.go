package durable

import (
	goai "github.com/rcarmo/go-ai"
	"sync"
	"time"
)

// generationProgress is the trailing 100ms partial publisher. Partials are
// detached before mark; one write runs at a time. Stop drops pending work and
// joins the actual write before terminal classification or host return.
type generationProgress struct {
	mu       sync.Mutex
	pending  *MessageReceipt
	stopped  bool
	wake     chan struct{}
	halt     chan struct{}
	done     chan struct{}
	after    func(time.Duration) <-chan time.Time // deterministic legacy actor tests
	now      func() time.Time
	arm      func(time.Duration) (<-chan time.Time, func(), error)
	interval time.Duration
	write    func(MessageReceipt) error
	report   func(error)
}

func newGenerationProgressClock(write func(MessageReceipt) error, report func(error), after func(time.Duration) <-chan time.Time) *generationProgress {
	return newGenerationProgressInterval(write, report, after, 100*time.Millisecond)
}
func newGenerationProgressInterval(write func(MessageReceipt) error, report func(error), after func(time.Duration) <-chan time.Time, interval time.Duration) *generationProgress {
	p := &generationProgress{interval: interval, wake: make(chan struct{}, 1), halt: make(chan struct{}), done: make(chan struct{}), after: after, write: write, report: report}
	go p.run()
	return p
}
func newRuntimeGenerationProgress(write func(MessageReceipt) error, report func(error), runtime *TaskRuntime, interval time.Duration) *generationProgress {
	now, arm := runtimeProgressTiming(runtime)
	p := &generationProgress{interval: interval, wake: make(chan struct{}, 1), halt: make(chan struct{}), done: make(chan struct{}), now: now, arm: arm, write: write, report: report}
	go p.run()
	return p
}
func (p *generationProgress) mark(message MessageReceipt) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.pending = &message
	select {
	case p.wake <- struct{}{}:
	default:
	}
}
func (p *generationProgress) stop() {
	p.mu.Lock()
	if !p.stopped {
		p.stopped = true
		p.pending = nil
		close(p.halt)
	}
	p.mu.Unlock()
	<-p.done
}
func (p *generationProgress) run() {
	defer close(p.done)
	for {
		select {
		case <-p.halt:
			return
		case <-p.wake:
		}
		// A mark during a write leaves a buffered wake even when the inner
		// loop already consumed its pending value. Do not arm an empty timer;
		// the next genuine mark must start its own publication window.
		p.mu.Lock()
		hasPending := p.pending != nil
		stopped := p.stopped
		p.mu.Unlock()
		if stopped {
			return
		}
		if !hasPending {
			continue
		}
		for {
			var until time.Time
			if p.now != nil {
				until = p.now().Add(p.interval)
			}
			armFailed := false
			for {
				delay := p.interval
				if p.now != nil {
					delay = max(time.Duration(0), until.Sub(p.now()))
				}
				var ticks <-chan time.Time
				disarm := func() {}
				if p.arm != nil {
					var err error
					ticks, disarm, err = p.arm(delay)
					if err != nil {
						// Partials are optional, never terminal authority. Drop this
						// failed publication window and allow a later mark to retry.
						p.mu.Lock()
						p.pending = nil
						p.mu.Unlock()
						if p.report != nil {
							p.report(err)
						}
						armFailed = true
						break
					}
				} else {
					ticks = p.after(delay)
				}
				select {
				case <-p.halt:
					disarm()
					return
				case <-ticks:
					disarm()
				}
				if p.now == nil || !p.now().Before(until) {
					break
				}
			}
			if armFailed {
				break
			}
			p.mu.Lock()
			if p.stopped {
				p.mu.Unlock()
				return
			}
			pending := p.pending
			p.pending = nil
			p.mu.Unlock()
			if pending != nil {
				if err := p.write(*pending); err != nil && p.report != nil {
					p.report(err)
				}
			}
			p.mu.Lock()
			dirty := p.pending != nil
			stopped := p.stopped
			p.mu.Unlock()
			if stopped {
				return
			}
			if !dirty {
				break
			}
		}
	}
}

func generationEventPartial(event goai.Event) *goai.Message {
	switch value := event.(type) {
	case *goai.StartEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.TextStartEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.TextDeltaEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.TextEndEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.ThinkingStartEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.ThinkingDeltaEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.ThinkingEndEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.ToolCallStartEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.ToolCallDeltaEvent:
		if value != nil {
			return value.Partial
		}
	case *goai.ToolCallEndEvent:
		if value != nil {
			return value.Partial
		}
	}
	return nil
}
