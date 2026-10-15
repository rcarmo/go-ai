package durable

import (
	"context"
	"sync"
)

// CodemodeRemote transports one execution with a bidirectional exchange. The
// transport must authenticate its peer, preserve ordering, honour cancellation
// and join remote work before Execute returns. Tool executors stay on the caller.
// This is the native Go protocol, not the TypeScript worker wire format.
type CodemodeRemote interface {
	Execute(context.Context, string, CodemodeStart, func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), func(context.Context) (CodemodeDelivery, error)) error
}
type CodemodeRemoteReply struct {
	Acknowledged *CodemodeDrain
	Stop         bool
}

// RemoteCodemodeSandbox runs only the VM remotely. Identity must identify the
// server's actual runtime/assets; it is persisted and checked on replay.
type RemoteCodemodeSandbox struct {
	Remote   CodemodeRemote
	Identity string
}

func (s *RemoteCodemodeSandbox) RuntimeIdentity() string           { return s.Identity }
func (*RemoteCodemodeSandbox) SupportsParallelCalls() bool         { return true }
func (*RemoteCodemodeSandbox) SupportsDrainAcknowledgements() bool { return true }
func (s *RemoteCodemodeSandbox) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	if ctx == nil || s == nil || s.Remote == nil || s.Identity == "" || start.RuntimeIdentity != "" && start.RuntimeIdentity != s.Identity {
		return nil, reject("invalid remote codemode runtime")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	linked, cancel := context.WithCancel(ctx)
	e := &remoteCodemodeExecution{ctx: linked, cancel: cancel, events: make(chan remoteCodemodeEvent, 1), replies: make(chan CodemodeRemoteReply), done: make(chan struct{}), deliveries: make(chan CodemodeDelivery)}
	// Detach all configuration before handing it to a transport implementation.
	object, err := dtoObject(start, DefaultLimits())
	if err != nil {
		cancel()
		return nil, err
	}
	var owned CodemodeStart
	if err := fromObject(object, &owned, DefaultLimits()); err != nil {
		cancel()
		return nil, err
	}
	go func() {
		defer close(e.done)
		terminal := false
		var terminalEvent *CodemodeEvent
		var exchangeMu sync.Mutex
		var protocolErr error
		err := callCodemodeRemote(linked, s.Remote, code, owned, func(ctx context.Context, event CodemodeEvent) (reply CodemodeRemoteReply, exchangeErr error) {
			exchangeMu.Lock()
			defer exchangeMu.Unlock()
			defer func() {
				if exchangeErr != nil && protocolErr == nil {
					protocolErr = exchangeErr
				}
			}()
			if protocolErr != nil {
				return CodemodeRemoteReply{Stop: true}, protocolErr
			}
			if terminal {
				return CodemodeRemoteReply{}, reject("remote event after terminal result")
			}
			fields := 0
			if event.Call != nil {
				fields++
			}
			if event.Drain != nil {
				fields++
			}
			if event.Result != nil {
				fields++
			}
			if fields != 1 {
				return CodemodeRemoteReply{}, reject("invalid remote codemode event")
			}
			if err := validateRemoteEventPayload(event); err != nil {
				return CodemodeRemoteReply{}, err
			}
			// Copy transport-owned maps before publication. Full output is projected by
			// the caller runtime, so it uses the existing codemode output ceiling here.
			limits := DefaultLimits()
			limits.MaxStringBytes = MaxCodemodeFullOutputBytes
			// Full text is transient here; durable persistence still applies its
			// normal document limit after projection/spill on the caller.
			limits.MaxDocumentBytes = 6*MaxCodemodeFullOutputBytes + 65536
			limits.MaxRecordBytes = limits.MaxDocumentBytes
			value, err := dtoObject(event, limits)
			if err != nil {
				return CodemodeRemoteReply{}, err
			}
			var detached CodemodeEvent
			if err := fromObject(value, &detached, limits); err != nil {
				return CodemodeRemoteReply{}, err
			}
			if err := e.admitEvent(detached, owned); err != nil {
				return CodemodeRemoteReply{}, err
			}
			if event.Result != nil {
				// Transport completion is part of terminal settlement. A peer
				// cannot publish success then hide a trailing protocol failure.
				terminal = true
				terminalEvent = &detached
				return CodemodeRemoteReply{Stop: true}, nil
			}
			select {
			case e.events <- remoteCodemodeEvent{event: detached}:
			case <-linked.Done():
				return CodemodeRemoteReply{Stop: true}, linked.Err()
			}
			// Call publication must never wait for its result: the following drain
			// is what authorises the host to admit effects.
			if event.Call != nil {
				return CodemodeRemoteReply{}, nil
			}
			select {
			case reply := <-e.replies:
				return reply, nil
			case <-linked.Done():
				return CodemodeRemoteReply{Stop: true}, linked.Err()
			}
		}, func(ctx context.Context) (CodemodeDelivery, error) {
			select {
			case delivery := <-e.deliveries:
				return delivery, nil
			case <-ctx.Done():
				return CodemodeDelivery{}, ctx.Err()
			case <-linked.Done():
				return CodemodeDelivery{}, linked.Err()
			}
		})
		exchangeMu.Lock()
		if protocolErr != nil {
			err = protocolErr
		}
		exchangeMu.Unlock()
		if err == nil && !terminal {
			err = reject("remote execution ended without result")
		}
		if err != nil {
			select {
			case e.events <- remoteCodemodeEvent{err: err}:
			case <-linked.Done():
			}
		} else if terminalEvent != nil {
			select {
			case e.events <- remoteCodemodeEvent{event: *terminalEvent}:
			case <-linked.Done():
			}
		}
	}()
	return e, nil
}
func callCodemodeRemote(ctx context.Context, remote CodemodeRemote, code string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("remote codemode transport panicked")
		}
	}()
	return remote.Execute(ctx, code, start, exchange, receive)
}

type remoteCodemodeEvent struct {
	event CodemodeEvent
	err   error
}
type remoteCodemodeExecution struct {
	ctx          context.Context
	cancel       context.CancelFunc
	events       chan remoteCodemodeEvent
	replies      chan CodemodeRemoteReply
	done         chan struct{}
	deliveries   chan CodemodeDelivery
	mu           sync.Mutex
	drain        *CodemodeDrain
	issued       int
	delivered    int
	lastDelivery int
	waiting      map[int]CodemodeCall
	acknowledged bool
	terminal     bool
}

func (e *remoteCodemodeExecution) Next(ctx context.Context) (CodemodeEvent, error) {
	if ctx == nil {
		return CodemodeEvent{}, reject("nil context")
	}
	select {
	case received := <-e.events:
		return received.event, received.err
	case <-ctx.Done():
		return CodemodeEvent{}, ctx.Err()
	case <-e.ctx.Done():
		return CodemodeEvent{}, e.ctx.Err()
	}
}
func (e *remoteCodemodeExecution) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	if ctx == nil {
		return reject("nil context")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil {
		return e.ctx.Err()
	}
	if e.terminal || e.drain == nil || *e.drain != drain {
		return reject("invalid remote drain acknowledgement")
	}
	select {
	case e.replies <- CodemodeRemoteReply{Acknowledged: &drain}:
		e.drain = nil
		e.acknowledged = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.ctx.Done():
		return e.ctx.Err()
	}
}
func (e *remoteCodemodeExecution) Deliver(ctx context.Context, delivery CodemodeDelivery) error {
	if ctx == nil {
		return reject("nil context")
	}
	object, err := dtoObject(delivery, DefaultLimits())
	if err != nil {
		return err
	}
	var owned CodemodeDelivery
	if err := fromObject(object, &owned, DefaultLimits()); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	call, ok := e.waiting[owned.Call.ID]
	if e.terminal || !e.acknowledged || !ok || owned.Seq != e.delivered+1 || owned.Now < 0 || owned.Call.Name != call.Name || owned.Call.Global != call.Global || !equalJSONValue(owned.Call.Arguments, call.Arguments) || owned.HasValue && owned.Error != "" || !owned.HasValue && owned.Value != nil {
		return reject("invalid remote delivery")
	}
	select {
	case e.deliveries <- owned:
		delete(e.waiting, call.ID)
		e.delivered++
		e.lastDelivery = call.ID
		e.acknowledged = false
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.ctx.Done():
		return e.ctx.Err()
	}
}
func (e *remoteCodemodeExecution) Close() error { e.cancel(); <-e.done; return nil }

func (e *remoteCodemodeExecution) admitEvent(event CodemodeEvent, start CodemodeStart) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.terminal {
		return reject("remote event after terminal result")
	}
	if event.Call != nil {
		if e.acknowledged || e.drain != nil {
			return reject("remote call outside drain window")
		}
		call := *event.Call
		if call.ID != e.issued+1 || call.ID > DefaultLimits().MaxPage || call.Arguments == nil {
			return reject("invalid remote call sequence")
		}
		allowed := false
		if call.Global {
			allowed = codemodeGlobalAllowed(start, call.Name)
		} else {
			for _, tool := range start.Tools {
				if tool.Name == call.Name {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			return reject("remote call outside captured authority")
		}
		args, err := copyObject(call.Arguments, DefaultLimits())
		if err != nil {
			return err
		}
		call.Arguments = args
		if e.waiting == nil {
			e.waiting = map[int]CodemodeCall{}
		}
		e.waiting[call.ID] = call
		e.issued++
	}
	if event.Drain != nil {
		if e.acknowledged || e.drain != nil || event.Drain.Issued != e.issued || event.Drain.DeliveryID != e.lastDelivery {
			return reject("invalid remote drain sequence")
		}
		copy := *event.Drain
		e.drain = &copy
	}
	if event.Result != nil {
		if event.Result.Error == "" && (!e.acknowledged || e.drain != nil) {
			return reject("remote success before drain acknowledgement")
		}
		e.terminal = true
	}
	return nil
}

// Only the output text gets a transient large-output allowance. All other
// fields retain the caller's ordinary JSON/result constraints.
func validateRemoteEventPayload(event CodemodeEvent) error {
	limits := DefaultLimits()
	if event.Call != nil {
		_, err := dtoObject(*event.Call, limits)
		return err
	}
	if event.Result != nil {
		if err := validateCodemodeFullText(event.Result.Output); err != nil {
			return err
		}
		bounded := *event.Result
		bounded.Output = ""
		if err := validateCodemodeResult(bounded, limits); err != nil {
			return err
		}
		_, err := dtoObject(bounded, limits)
		return err
	}
	return nil
}
