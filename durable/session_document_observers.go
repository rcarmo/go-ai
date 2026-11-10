package durable

import (
	"context"
	"errors"
	"sync"
)

// Private synchronous observeDoc slice. Its callbacks run on the Session line
// and must not block or synchronously call Session/Storage APIs. Public API
// ownership and re-entry semantics remain separate work.
type sessionDocumentObserver struct {
	advance         func(context.Context, WatchFrame) error
	readonlyAdvance func(context.Context, DocumentCall) error
	initialReadonly func(ReadonlyValue)
	end             func(WatchEnd) error
	report          func(context.Context, error, ReportInfo)
}

type sessionDocumentObservation struct {
	mu       sync.Mutex
	session  *Session
	observer sessionDocumentObserver
	listener *sessionCommitListener
	address  DocumentAddress
	id       ID
	version  uint64
	readonly ReadonlyValue
	value    JSON  // current followed value; cleared on retirement/detach
	quota    int64 // immutable registration reservation, read under observerMu
	limits   Limits
}

// observeDocumentDefinition follows an existing typed address without creating
// it. Attachment and baseline are captured together under the Session line.
// Its context governs acquisition only, not the lifetime of the returned guard.
func (s *Session) observeDocumentDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string, observer sessionDocumentObserver) (*sessionDocumentObservation, JSON, bool, error) {
	if (observer.advance == nil && observer.readonlyAdvance == nil) || observer.end == nil {
		return nil, nil, false, reject("incomplete synchronous document observer")
	}
	if err := s.enter(ctx); err != nil {
		return nil, nil, false, err
	}
	defer s.leave()
	// Upstream gates every read: a job must not observe state an admitted
	// write has not published.
	if err := s.touchAll(ctx); err != nil {
		return nil, nil, false, err
	}
	// Validate the address before checking owner liveness: malformed task
	// scopes and family keys must reject, not appear to be absent.
	address, err := definitionAddress(def, owner, key)
	if err != nil {
		return nil, nil, false, err
	}
	// A task-scoped document is observable only while its owner is live.
	// Task records may remain after terminal retirement or corrupted input;
	// do not create an observation or pin for a non-live owner.
	if address.Scope == "task" {
		state, err := s.taskSnapshot(ctx)
		if err != nil {
			return nil, nil, false, err
		}
		task, ok := state.Tasks[address.Owner]
		if !ok || terminalStatus(task.Status) {
			return nil, nil, false, nil
		}
	}
	record, value, exists, err := s.watchDefinitionValue(ctx, def, owner, key)
	if err != nil {
		return nil, nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	if !exists {
		return nil, nil, false, nil
	}
	capacity := s.observerCapacity()
	quota := s.limits.MaxRetainedBytes / int64(capacity)
	encoded, err := encodeBounded(JSON{"value": value}, s.limits, s.limits.MaxFramePayloadBytes)
	if err != nil {
		return nil, nil, false, err
	}
	if int64(len(encoded)) > quota {
		return nil, nil, false, reject("document observer baseline exceeds process quota")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	initial, err := copyObject(value, s.limits)
	if err != nil {
		return nil, nil, false, err
	}
	var frozen ReadonlyValue
	if observer.readonlyAdvance != nil {
		frozen, err = s.documentObservationReadonly(record.ID, def.options.Version, value)
		if err != nil {
			return nil, nil, false, err
		}
	}
	if observer.initialReadonly != nil {
		observer.initialReadonly(frozen)
		observer.initialReadonly = nil
	}
	observation := &sessionDocumentObservation{readonly: frozen, session: s, observer: observer, address: documentAddress(record), id: record.ID, version: def.options.Version, value: value, quota: quota, limits: s.limits}
	s.observerMu.Lock()
	if err := s.failure(); err != nil {
		s.observerMu.Unlock()
		return nil, nil, false, err
	}
	if s.closing.Load() {
		s.observerMu.Unlock()
		return nil, nil, false, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		s.observerMu.Unlock()
		return nil, nil, false, err
	}
	if s.observerProcessCountLocked() >= capacity || quota > s.limits.MaxRetainedBytes-s.observerReservedBytesLocked() {
		s.observerMu.Unlock()
		return nil, nil, false, reject("document observer process limit")
	}
	listener := &sessionCommitListener{document: observation}
	observation.listener = listener
	s.documentObservers = append(s.documentObservers, observation)
	s.localCommitListeners = append(s.localCommitListeners, listener)
	s.observerMu.Unlock()
	// The source checks again after attachment and detaches on cancellation.
	// Never call release while holding observerMu.
	if err := ctx.Err(); err != nil {
		observation.detach()
		return nil, nil, false, err
	}
	return observation, initial, true, nil
}

// Release references before invoking a host end callback. Lock ordering never
// holds the guard mutex while acquiring the Session registry mutex.
func (o *sessionDocumentObservation) release() (sessionDocumentObserver, bool) {
	o.mu.Lock()
	s, observer, listener := o.session, o.observer, o.listener
	o.session, o.observer, o.listener, o.value, o.id = nil, sessionDocumentObserver{}, nil, nil, 0
	o.readonly = nil
	o.mu.Unlock()
	if s == nil {
		return sessionDocumentObserver{}, false
	}
	s.observerMu.Lock()
	for i, candidate := range s.documentObservers {
		if candidate == o {
			copy(s.documentObservers[i:], s.documentObservers[i+1:])
			last := len(s.documentObservers) - 1
			s.documentObservers[last] = nil
			s.documentObservers = s.documentObservers[:last]
			if last == 0 {
				s.documentObservers = nil
			}
			break
		}
	}
	for i, candidate := range s.localCommitListeners {
		if candidate == listener {
			copy(s.localCommitListeners[i:], s.localCommitListeners[i+1:])
			last := len(s.localCommitListeners) - 1
			s.localCommitListeners[last] = nil
			s.localCommitListeners = s.localCommitListeners[:last]
			if last == 0 {
				s.localCommitListeners = nil
			}
			break
		}
	}
	s.observerMu.Unlock()
	// Detach may run inside a synchronous publication on the Session line.
	// Schedule retention bookkeeping off-line, as DocumentWatch does, so a
	// previously pinned cache entry can expire without re-entering the line.
	if !s.closing.Load() {
		go s.taskBookkeeping(func() {
			if !s.closing.Load() {
				s.sweepDocumentCache()
				s.scheduleDocumentExpiry()
			}
		})
	}
	return observer, true
}
func (o *sessionDocumentObservation) detach() { _, _ = o.release() }

func reportDocumentObserver(observer sessionDocumentObserver, ctx context.Context, err error) {
	if observer.report != nil {
		_ = callSessionCommitObserver(func() error { observer.report(ctx, err, ReportInfo{Type: "listener_error"}); return nil })
	}
}
func endDocumentObserver(observer sessionDocumentObserver, end WatchEnd) {
	if err := callSessionCommitObserver(func() error { return observer.end(end) }); err != nil {
		reportDocumentObserver(observer, context.Background(), err)
	}
}
func (o *sessionDocumentObservation) fail(ctx context.Context, observer sessionDocumentObserver, err error) {
	reportDocumentObserver(observer, ctx, err)
	if _, released := o.release(); released {
		endDocumentObserver(observer, WatchEnd{Reason: "listener_error", Error: err})
	}
}

// Each change is delivered separately. Recheck attachment on every iteration:
// an earlier callback may detach this guard or a later captured guard.
func (o *sessionDocumentObservation) advance(ctx context.Context, publication CommitPublication, shared ...[]ReadonlyValue) {
	ownerEnd := false
	if o.address.Scope == "task" {
		for _, write := range publication.Tables {
			if write.Task != nil && write.Task.ID == o.address.Owner && terminalStatus(write.Task.Status) {
				ownerEnd = true
			}
		}
	}
	for index, change := range publication.Documents {
		o.mu.Lock()
		if o.session == nil {
			o.mu.Unlock()
			return
		}
		if change.Record.ID != o.id && documentAddress(change.Record) != o.address {
			o.mu.Unlock()
			continue
		}
		observer := o.observer
		var value JSON
		var operations []Operation
		retired := change.Record.Retired
		var err error
		if retired {
			if change.Record.ID != o.id {
				o.mu.Unlock()
				continue
			}
			value, err = copyObjectNullable(o.value, o.limits)
			operations = []Operation{}
			o.id, o.value = 0, nil
		} else {
			if change.Record.ID != o.id || change.Version != o.version {
				operations = []Operation{{"r", mapOrNull(change.Value)}}
			} else if len(change.Ops) == 0 {
				o.mu.Unlock()
				continue
			} else {
				operations = change.Ops
			}
			value, err = copyObjectNullable(change.Value, o.limits)
			if err == nil {
				o.value, err = copyObjectNullable(change.Value, o.limits)
			}
			o.id, o.version = change.Record.ID, change.Version
		}
		if err == nil {
			operations, err = ownOperations(operations, o.limits)
		}
		if err == nil {
			var encoded []byte
			encoded, err = encodeBounded(JSON{"value": value, "ops": operations}, o.limits, o.limits.MaxFramePayloadBytes)
			if err == nil && int64(len(encoded)) > o.quota {
				err = reject("document observer publication exceeds process quota")
			}
		}
		if err != nil {
			o.id, o.value = 0, nil
		}
		var frozen ReadonlyValue
		if err == nil && observer.readonlyAdvance != nil {
			if retired {
				frozen = o.readonly
				o.readonly = nil
			} else {
				if len(shared) > 0 {
					frozen = shared[0][index]
				}
				if frozen == nil {
					var node *ImmutableValue
					if o.readonly == nil || len(operations) == 0 || operations[0][0] == "r" {
						node, err = NewImmutableValue(value, o.limits)
					} else {
						node, err = applyImmutableValue(o.readonly.immutableNode(), operations, o.limits)
					}
					if err == nil {
						frozen = readonlyValue(node)
						if len(shared) > 0 {
							shared[0][index] = frozen
						}
					}
				}
				o.readonly = frozen
			}
		}
		o.mu.Unlock()
		if err != nil {
			o.fail(ctx, observer, err)
			return
		}
		frame := WatchFrame{Kind: "value", Retired: retired, Seq: publication.Seq, Value: value, Ops: operations, Context: ctx}
		if err := callSessionCommitObserver(func() error {
			if observer.readonlyAdvance != nil {
				return observer.readonlyAdvance(ctx, DocumentCall{Kind: "value", Value: frozen, Ops: operations, Retired: retired})
			}
			return observer.advance(ctx, frame)
		}); err != nil {
			o.fail(ctx, observer, err)
			return
		}
	}
	if ownerEnd {
		if observer, released := o.release(); released {
			endDocumentObserver(observer, WatchEnd{Reason: "ended"})
		}
	}
}

func (s *Session) endDocumentObservers() {
	s.failureMu.Lock()
	cause := s.failureCause
	s.failureMu.Unlock()
	if cause == nil {
		cause = s.closeErr
	}
	if cause == nil {
		cause = errors.New("session closed")
	}
	s.observerMu.Lock()
	observers := append([]*sessionDocumentObservation(nil), s.documentObservers...)
	s.observerMu.Unlock()
	for _, observation := range observers {
		if observer, released := observation.release(); released {
			endDocumentObserver(observer, WatchEnd{Reason: "failed", Error: cause})
		}
	}
}

// Registration runs on the Session line. Existing address/version observers
// already own its adopted readonly node; attachment must reuse that identity.
func (s *Session) documentObservationReadonly(id ID, version uint64, value JSON) (ReadonlyValue, error) {
	s.observerMu.Lock()
	for _, observation := range s.documentObservers {
		observation.mu.Lock()
		node := observation.readonly
		matches := observation.id == id && observation.version == version && node != nil
		observation.mu.Unlock()
		if matches {
			s.observerMu.Unlock()
			return node, nil
		}
	}
	s.observerMu.Unlock()
	node, err := NewImmutableValue(value, s.limits)
	if err != nil {
		return nil, err
	}
	return readonlyValue(node), nil
}
