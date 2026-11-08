package durable

import (
	"context"
	"sync"
)

// Internal port of pinned v1.5.0 src/session/writer.ts. This queue is not yet
// installed on Session: admission, keyed reads, close and failure boundaries
// must move together. Native receipt frames preserve exact-prefix adoption;
// neither a backend head snapshot nor a caller's image substitutes for them.
type writerPublishFailure struct {
	adoption bool // false: own listener failure after successful adoption
	err      error
}
type sessionWriterSpec struct {
	index   uint64
	batch   *Batch // nil: held/memory-only part; non-nil must have writes
	keys    []string
	ctx     context.Context
	publish func(*commitReceipt) *writerPublishFailure // nil receipt for held part
	discard func()
}
type sessionWriterPart struct {
	spec    sessionWriterSpec
	outcome chan struct{}
	gate    chan struct{}
	err     error // immutable after outcome closes
	done    bool  // flush goroutine only
}
type sessionWriterHost struct {
	commit func(context.Context, []Batch, func(commitReceipt)) (commitGroupResult, error)
	// Runs one part's publication inside the Storage's own publication scope.
	// It returns nil when the publication ran synchronously, otherwise a channel
	// closed once it ran. The host owns the pinned `#publishing` contract: a scope
	// that throws instead of running the publication is reported by the host and the
	// publication then runs directly, because the commit is stored and must be
	// published. The closure must not hold any lock admit/gate/drained need.
	publishing func(context.Context, func()) <-chan struct{}
	failure    func() error // first cause, not a SessionFailed wrapper
	fail       func(error, uint64, []uint64)
	report     func(error, context.Context)
	// Host-controlled publication turn boundary. Go has no JS macrotasks;
	// Session integration must explicitly establish continuation ordering here.
	hop func()
}
type sessionWriter struct {
	host        sessionWriterHost
	mu          sync.Mutex
	queue       []*sessionWriterPart
	flight      bool
	unpublished map[string]*sessionWriterPart
	newest      *sessionWriterPart
}

func newSessionWriter(host sessionWriterHost) *sessionWriter {
	return &sessionWriter{host: host, unpublished: make(map[string]*sessionWriterPart)}
}

// Ownership of spec's sealed batch/callbacks transfers at admission. No host
// callback runs with mu held; callbacks may reenter gate/drained/admit.
func (w *sessionWriter) admit(spec sessionWriterSpec) *sessionWriterPart {
	spec.keys = append([]string(nil), spec.keys...)
	p := &sessionWriterPart{spec: spec, outcome: make(chan struct{}), gate: make(chan struct{})}
	w.mu.Lock()
	for _, key := range spec.keys {
		w.unpublished[key] = p
	}
	w.newest = p
	w.queue = append(w.queue, p)
	w.pumpLocked()
	w.mu.Unlock()
	return p
}
func (w *sessionWriter) gate(key string) <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p := w.unpublished[key]; p != nil {
		return p.gate
	}
	return nil
}
func (w *sessionWriter) drained() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.newest != nil {
		return w.newest.gate
	}
	return nil
}
func (w *sessionWriter) unpublishedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.unpublished)
}
func (w *sessionWriter) pumpLocked() {
	if w.flight || len(w.queue) == 0 {
		return
	}
	parts := w.queue
	w.queue = nil
	w.flight = true
	go w.flush(parts)
}
func (w *sessionWriter) flush(parts []*sessionWriterPart) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// Do not stringify arbitrary panic payloads (they may contain private data).
			cause := reject("writer host panicked")
			w.host.fail(cause, parts[0].spec.index, nil)
			for _, p := range parts {
				if !p.done {
					w.unpublish(p, w.failed(cause))
				}
			}
		}
		w.mu.Lock()
		w.flight = false
		w.pumpLocked()
		w.mu.Unlock()
	}()
	w.send(parts)
}
func (w *sessionWriter) send(parts []*sessionWriterPart) {
	if cause := w.host.failure(); cause != nil {
		for _, p := range parts {
			w.unpublish(p, &SessionFailed{Cause: cause})
		}
		return
	}
	var batches []Batch
	for _, p := range parts {
		if p.spec.batch != nil {
			batches = append(batches, *p.spec.batch)
		}
	}
	receipts := make(map[int]commitReceipt)
	var result commitGroupResult
	var rejected error
	if len(batches) > 0 {
		result, rejected = w.host.commit(context.WithoutCancel(partsContext(parts)), batches, func(r commitReceipt) { receipts[r.index] = r })
	}
	if cause := w.host.failure(); cause != nil {
		for _, p := range parts {
			w.unpublish(p, &SessionFailed{Cause: cause})
		}
		return
	}
	w.publish(parts, receipts, result, rejected)
}
func partsContext(parts []*sessionWriterPart) context.Context {
	for _, p := range parts {
		if p.spec.batch != nil {
			return p.spec.ctx
		}
	}
	return parts[0].spec.ctx
}

type writerFailedPart struct {
	part      *sessionWriterPart
	own       error // nil: SessionFailed
	published bool
}

func (w *sessionWriter) publish(parts []*sessionWriterPart, receipts map[int]commitReceipt, result commitGroupResult, rejected error) {
	var stop error
	var at uint64
	external := false
	var failing []writerFailedPart
	storedIndex, published := 0, 0
	for _, p := range parts {
		index := -1
		if p.spec.batch != nil {
			index = storedIndex
			storedIndex++
		}
		if stop != nil {
			var own error
			if rejected != nil && index >= 0 && !external {
				own = rejected
			}
			failing = append(failing, writerFailedPart{part: p, own: own})
			continue
		}
		var receipt *commitReceipt
		if index >= 0 {
			r, ok := receipts[index]
			if !ok {
				cause := rejected
				if cause == nil && index == result.stored {
					cause = result.refusalError()
				}
				if cause == nil {
					cause = reject("storage committed without adopting the batch")
				}
				failing = append(failing, writerFailedPart{part: p, own: cause})
				stop, at = cause, p.spec.index
				continue
			}
			receipt = &r
		}
		if published > 0 {
			w.host.hop()
		}
		published++
		var failure *writerPublishFailure
		var outside error
		// The failure is rechecked immediately inside the scope, at the moment it
		// runs rather than when it was asked for: a deferred scope can enter before
		// something else fails the Session and run the publication after it.
		done := w.host.publishing(p.spec.ctx, func() {
			outside = w.host.failure()
			if outside != nil {
				return
			}
			w.unregister(p)
			failure = callWriterPublication(p.spec.publish, receipt)
		})
		if done != nil {
			<-done
		}
		if outside != nil {
			stop, at, external = outside, p.spec.index, true
			failing = append(failing, writerFailedPart{part: p})
			continue
		}
		if failure != nil && failure.adoption {
			failing = append(failing, writerFailedPart{part: p, own: failure.err, published: true})
			stop, at = failure.err, p.spec.index
			continue
		}
		if rejected != nil {
			if failure != nil {
				w.host.report(failure.err, p.spec.ctx)
			}
			failing = append(failing, writerFailedPart{part: p, own: rejected, published: true})
			continue
		}
		if failure != nil {
			stop, at = failure.err, p.spec.index
		}
		var err error
		if cause := w.host.failure(); cause != nil && failure == nil {
			err = &SessionFailed{Cause: cause}
		}
		w.settle(p, err)
	}
	if rejected != nil || stop != nil {
		var own []uint64
		for _, f := range failing {
			if f.own != nil {
				own = append(own, f.part.spec.index)
			}
		}
		if rejected != nil {
			w.host.fail(rejected, own[0], own)
		} else {
			w.host.fail(stop, at, own)
		}
	}
	for _, f := range failing {
		err := f.own
		if err == nil {
			err = w.failed(stop)
		}
		if f.published {
			w.settle(f.part, err)
		} else {
			w.unpublish(f.part, err)
		}
	}
	if cause := w.host.failure(); cause != nil {
		w.mu.Lock()
		queued := w.queue
		w.queue = nil
		w.mu.Unlock()
		for _, p := range queued {
			w.unpublish(p, &SessionFailed{Cause: cause})
		}
	}
}
func callWriterPublication(publish func(*commitReceipt) *writerPublishFailure, r *commitReceipt) (failure *writerPublishFailure) {
	defer func() {
		if recover() != nil {
			failure = &writerPublishFailure{adoption: true, err: reject("writer publication panicked")}
		}
	}()
	return publish(r)
}
func (w *sessionWriter) failed(fallback error) error {
	if cause := w.host.failure(); cause != nil {
		return &SessionFailed{Cause: cause}
	}
	return &SessionFailed{Cause: fallback}
}
func (w *sessionWriter) unregister(p *sessionWriterPart) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, key := range p.spec.keys {
		if w.unpublished[key] == p {
			delete(w.unpublished, key)
		}
	}
}
func (w *sessionWriter) settle(p *sessionWriterPart, err error) {
	if p.done {
		return
	}
	p.err, p.done = err, true
	close(p.outcome)
	w.mu.Lock()
	if w.newest == p {
		w.newest = nil
	}
	close(p.gate)
	w.mu.Unlock()
}
func (w *sessionWriter) unpublish(p *sessionWriterPart, err error) {
	// Discard must not strand gates even if a buggy host cleanup panics.
	func() { defer func() { _ = recover() }(); p.spec.discard() }()
	w.unregister(p)
	w.settle(p, err)
}
