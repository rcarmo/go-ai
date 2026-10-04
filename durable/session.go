package durable

import (
	"context"
	"sync"
	"sync/atomic"
)

// Session serializes transaction preparation, storage settlement and committed
// reads. M1a has no execution/scheduling API and dispatches no external effects.
type Session struct {
	store                Storage
	core                 *storeCore
	line                 chan struct{}
	closing              atomic.Bool
	once                 sync.Once
	done                 chan struct{}
	closeErr             error
	limits               Limits
	definitionCache      map[definitionCacheKey]definitionCacheValue
	definitionCacheBytes int64
	observerMu           sync.Mutex
	watches              map[*DocumentWatch]bool
	subscriptions        map[*CommitSubscription]bool
}

// OpenSession claims one native memory/journal store. Storage remains owned
// until the common Close operation drains admitted transactions and settles.
func OpenSession(store Storage) (*Session, error) {
	if store == nil {
		return nil, reject("nil storage")
	}
	var core *storeCore
	switch native := store.(type) {
	case *MemoryStorage:
		core = native.storeCore
	case *JournalStorage:
		core = native.storeCore
	default:
		return nil, reject("M1a requires native storage")
	}
	if e := core.claim(); e != nil {
		return nil, e
	}
	l, e := store.Limits()
	if e != nil {
		return nil, e
	}
	s := &Session{store: store, core: core, line: make(chan struct{}, 1), done: make(chan struct{}), limits: l}
	s.line <- struct{}{}
	return s, nil
}
func (s *Session) enter(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	if s.closing.Load() {
		return ErrClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.line:
	}
	if s.closing.Load() {
		s.leave()
		return ErrClosed
	}
	if e := ctx.Err(); e != nil {
		s.leave()
		return e
	}
	return nil
}
func (s *Session) leave() { s.line <- struct{}{} }
func (s *Session) Snapshot(ctx context.Context) (Snapshot, error) {
	if e := s.enter(ctx); e != nil {
		return Snapshot{}, e
	}
	defer s.leave()
	return s.store.Snapshot(ctx)
}
func (s *Session) MintID(ctx context.Context) (ID, error) {
	if e := s.enter(ctx); e != nil {
		return 0, e
	}
	defer s.leave()
	return s.core.mintID(ctx, true)
}

// Commit seals every escaped handle after callback return, including panic. A
// canceled preparation may reject before Apply; after storage admission the
// native store completes adopt-or-poison without releasing the mutation line.
func (s *Session) Commit(ctx context.Context, callback func(*Tx) error) (seq uint64, err error) {
	if callback == nil {
		return 0, reject("nil transaction callback")
	}
	if err = s.enter(ctx); err != nil {
		return 0, err
	}
	ready := make(chan struct{})
	defer func() { s.leave(); close(ready); s.kickWatches() }()
	state, e := s.store.Snapshot(ctx)
	if e != nil {
		return 0, e
	}
	tx := &Tx{store: s.core, ctx: ctx, limits: s.limits, state: state, session: s}
	defer tx.seal()
	if err = callback(tx); err != nil {
		return 0, err
	}
	if err = tx.seal(); err != nil {
		return 0, err
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	if err = tx.finalizeDocuments(); err != nil {
		return 0, err
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	if len(tx.writes) == 0 {
		return state.Seq, nil
	}
	publication, e := tx.preparePublication(ctx)
	if e != nil {
		return 0, e
	}
	frames, e := s.prepareWatchFrames(ctx, publication, ready)
	if e != nil {
		return 0, e
	}
	subs, e := s.prepareSubscriptions(ctx, publication, tx, ready)
	if e != nil {
		return 0, e
	}
	seq, err = s.core.apply(ctx, Batch{Writes: tx.writes}, true)
	if err == nil {
		s.enqueueWatchFrames(frames)
		s.enqueueSubscriptions(subs)
		// Only changed incarnations invalidate migration cache entries. An
		// unrelated commit never turns a warm migration into a cold load.
		for _, write := range tx.writes {
			var changed ID
			if write.Document != nil {
				changed = write.Document.ID
			}
			if write.Delta != nil {
				changed = write.Delta.ID
			}
			if changed == 0 {
				continue
			}
			for key, value := range s.definitionCache {
				if key.id == changed {
					delete(s.definitionCache, key)
					s.definitionCacheBytes -= value.bytes
				}
			}
		}
	}
	return seq, err
}
func (s *Session) Close(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	s.once.Do(func() {
		s.closing.Store(true)
		s.closeWatches()
		go func() { <-s.line; s.closeErr = s.core.close(context.Background(), true); close(s.done); s.leave() }()
	})
	select {
	case <-s.done:
		return s.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
