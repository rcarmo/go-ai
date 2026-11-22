package durable

import (
	"context"
	"sync/atomic"
)

type publicationContinuationKey struct{}

// PublicationTurn is the synchronous continuation of a successfully stored
// and published Session commit. Read methods are valid only during the callback.
type PublicationTurn struct {
	Sequence uint64
	Value    any
	session  *Session
	ctx      context.Context
	index    uint64
	sealed   atomic.Bool
	busy     atomic.Bool
}

// Task reads a task that is not keyed by a later, unpublished part. It will
// not wait for a publication that cannot advance until this callback returns.
// A backend read failure fences the Session even if the callback ignores it.
func (p *PublicationTurn) Task(id ID) (TaskRecord, bool, error) {
	if p == nil || p.session == nil || p.sealed.Load() {
		return TaskRecord{}, false, ErrSealed
	}
	if !p.busy.CompareAndSwap(false, true) {
		return TaskRecord{}, false, ErrConcurrent
	}
	defer p.busy.Store(false)
	if p.sealed.Load() {
		return TaskRecord{}, false, ErrSealed
	}
	if err := checkReadIDs(id); err != nil {
		return TaskRecord{}, false, err
	}
	s := p.session
	if s.writer.gate(taskKey(id)) != nil {
		return TaskRecord{}, false, reject("publication continuation cannot await an unpublished task")
	}
	if err := s.failure(); err != nil {
		return TaskRecord{}, false, err
	}
	value, found, err := s.store.Task(p.ctx, id)
	return value, found, s.backendResultAt(p.ctx, err, false, &p.index, nil)
}

// CommitWithContinuation is an immediate Commit with an explicit Go turn
// before the writer can publish another part. The successful result becomes
// available before then runs; then may overlap the method's return. It runs
// outside the Session line, but must not await new Session jobs or Close.
// A returned error or panic fails the Session without revoking this result.
// No-write, refused, or rejected commits do not run then.
func (s *Session) CommitWithContinuation(ctx context.Context, change func(*Tx) (any, error), then func(*PublicationTurn) error) (any, error) {
	if ctx == nil || change == nil || then == nil {
		return nil, reject("publication continuation requires a context and two callbacks")
	}
	var value any
	continuation := func(turn *PublicationTurn) error {
		turn.Value = value
		return then(turn)
	}
	return s.Commit(context.WithValue(ctx, publicationContinuationKey{}, continuation), func(tx *Tx) (any, error) {
		var err error
		value, err = change(tx)
		return value, err
	})
}

func (s *Session) writerContinuation(ctx context.Context, index uint64) func(commitReceipt) error {
	then, _ := ctx.Value(publicationContinuationKey{}).(func(*PublicationTurn) error)
	if then == nil {
		return nil
	}
	return func(receipt commitReceipt) (err error) {
		turn := &PublicationTurn{Sequence: receipt.seq, session: s, ctx: context.WithoutCancel(ctx), index: index}
		defer func() {
			turn.sealed.Store(true)
			if value := recover(); value != nil {
				err = newFailure(value).AsError()
			}
		}()
		return then(turn)
	}
}
