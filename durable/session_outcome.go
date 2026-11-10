package durable

import "sync"

// sessionOutcome is #enqueue's delivery fence. Memory/admission locks are not
// held while delivering. The previous outcome already follows its predecessors.
type sessionOutcome struct {
	index              uint64
	previous           <-chan struct{}
	done               chan struct{}
	once               sync.Once
	exempt, hit, after bool // job-owned until line part ends
	release            func(bool)
}

func (s *Session) enqueueOutcome() *sessionOutcome {
	index := s.jobIndex()
	outcome := &sessionOutcome{index: index, previous: s.deliveryTail, done: make(chan struct{}), release: func(fenced bool) {
		if fenced {
			s.failureMu.Lock()
			s.lastDelivered = index
			s.failureMu.Unlock()
		}
		<-s.outcomeSlots
	}}
	s.deliveryTail = outcome.done
	return outcome
}

func (o *sessionOutcome) deliver() {
	if o == nil {
		return
	}
	o.once.Do(func() {
		if o.exempt && !o.hit && !o.after {
			// The exempt outcome is delivered now, but the ordered fence still
			// follows its predecessor for every later non-exempt job.
			o.release(false)
			if o.previous == nil {
				close(o.done)
			} else {
				go func() { <-o.previous; close(o.done) }()
			}
			return
		}
		if o.previous != nil {
			<-o.previous
		}
		o.release(true)
		close(o.done)
	})
}
