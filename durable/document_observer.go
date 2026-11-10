package durable

import "context"

// DocumentObserver receives unfolded document changes as they are adopted.
// Advance runs synchronously on the Session line: it must be quick and must
// not block or call Session APIs. An error or panic detaches this observer and
// ends it with listener_error without failing the commit. Values are shared
// readonly handles; operation containers are callback-owned. End is called
// once unless the observation is detached.
type DocumentObserver struct {
	Advance func(context.Context, DocumentCall) error
	End     func(WatchEnd) error
}

// DocumentCall is one synchronous adopted document revision. Retirement carries
// the preceding readonly value, and recreation replaces it as a whole.
type DocumentCall struct {
	Kind    string        `json:"kind"`
	Value   ReadonlyValue `json:"value"`
	Ops     []Operation   `json:"ops"`
	Retired bool          `json:"retired"`
}

// DocumentObservation holds the readonly value at attachment, not a live view.
// Detach is idempotent and suppresses subsequent advances and end callbacks.
type DocumentObservation struct {
	Value ReadonlyValue
	guard *sessionDocumentObservation
}

func (o *DocumentObservation) Detach() {
	if o != nil && o.guard != nil {
		o.guard.detach()
	}
}

// ObserveDefinition atomically attaches to an existing typed document address.
// A missing document returns (nil, false, nil) and creates no observation.
// Context cancellation applies to attachment, not to the observation lifetime.
func (s *Session) ObserveDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string, observer DocumentObserver) (*DocumentObservation, bool, error) {
	observation := &DocumentObservation{}
	guard, _, found, err := s.observeDocumentDefinition(ctx, def, owner, key, sessionDocumentObserver{
		readonlyAdvance: observer.Advance,
		initialReadonly: func(value ReadonlyValue) { observation.Value = value },
		end:             observer.End,
		report: func(ctx context.Context, err error, _ ReportInfo) {
			s.reportListener(ctx, err)
		},
	})
	if err != nil || !found {
		return nil, found, err
	}
	observation.guard = guard
	return observation, true, nil
}
