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
	mu      sync.Mutex
	pending *MessageReceipt
	stopped bool
	wake    chan struct{}
	halt    chan struct{}
	done    chan struct{}
	after   func(time.Duration) <-chan time.Time
	write   func(MessageReceipt) error
	report  func(error)
}

func newGenerationProgress(write func(MessageReceipt) error, report func(error)) *generationProgress {
	return newGenerationProgressClock(write, report, time.After)
}
func newGenerationProgressClock(write func(MessageReceipt) error, report func(error), after func(time.Duration) <-chan time.Time) *generationProgress {
	p := &generationProgress{wake: make(chan struct{}, 1), halt: make(chan struct{}), done: make(chan struct{}), after: after, write: write, report: report}
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
			select {
			case <-p.halt:
				return
			case <-p.after(100 * time.Millisecond):
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
