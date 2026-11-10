package durable

import "sync"

// sessionOutcome is #enqueue's delivery fence. Memory/admission locks are not
// held while delivering. The previous outcome already follows its predecessors.
type sessionOutcome struct {
	previous <-chan struct{}
	done     chan struct{}
	once     sync.Once
}

func (s *Session) enqueueOutcome() *sessionOutcome {
	outcome := &sessionOutcome{previous: s.deliveryTail, done: make(chan struct{})}
	s.deliveryTail = outcome.done
	return outcome
}

func (o *sessionOutcome) deliver() {
	if o == nil {
		return
	}
	o.once.Do(func() {
		if o.previous != nil {
			<-o.previous
		}
		close(o.done)
	})
}
