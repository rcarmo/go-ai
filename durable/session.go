package durable

import (
	"context"
	"errors"
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
	taskScheduler        *taskScheduler // private line-owned Harness integration
}

// OpenSession claims one native memory/journal store. Storage remains owned
// until the common Close operation drains admitted transactions and settles.
func OpenSession(store Storage) (*Session, error) {
	if store == nil {
		return nil, reject("nil storage")
	}
	var core *storeCore
	native, ok := store.(nativeStorage)
	if !ok {
		return nil, reject("session requires native storage")
	}
	core = native.nativeCore()
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
func (s *Session) Commit(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	return s.commit(ctx, callback, true, 0)
}

func (s *Session) taskCommit(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	return s.commit(ctx, callback, false, 0)
}
func (s *Session) invocationCommit(ctx context.Context, taskID ID, callback func(*Tx) error) (uint64, error) {
	return s.commit(ctx, callback, false, taskID)
}

// Internal scheduler/adapter admissions cannot create their own retry epoch.
// Runtime admission identity witnesses a failed apply BEFORE storage, retaining
// a genuine external wake that arrives while the failed storage is settling.
func (s *Session) commit(ctx context.Context, callback func(*Tx) error, external bool, taskID ID) (seq uint64, err error) {
	if callback == nil {
		return 0, reject("nil transaction callback")
	}
	if err = s.enter(ctx); err != nil {
		return 0, err
	}
	markAdmission := func() {
		if s.taskScheduler != nil && taskID != 0 {
			if r := s.taskScheduler.invocations[taskID]; r != nil {
				r.admissionEpoch = s.taskScheduler.epoch.Load()
				if r.fallbackEpoch != nil {
					r.admissionEpoch = *r.fallbackEpoch
				}
			}
		}
	}
	markAdmission()
	ready := make(chan struct{})
	var tx *Tx
	defer func() {
		if err != nil && tx != nil && tx.taskRollback != nil {
			tx.taskRollback(err)
		}
		if s.taskScheduler != nil && err != nil && errors.Is(err, ErrPoisoned) {
			s.taskScheduler.notifyWaiters(ErrPoisoned)
			for target := range s.taskScheduler.tickets {
				s.taskScheduler.failTicket(target, ErrPoisoned)
			}
		}
		s.leave()
		close(ready)
		s.kickWatches()
		if s.taskScheduler != nil {
			s.taskScheduler.dispatchStops()
		}
	}()
	state, e := s.taskSnapshot(ctx)
	if e != nil {
		err = e
		return 0, e
	}
	tx = &Tx{store: s.core, ctx: ctx, limits: s.limits, state: state, session: s, externalTaskCommit: external}
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
	// Task owner validation and same-batch retirement precede document content
	// selection. This private sealed-Tx path executes no task/host callbacks.
	if err = s.finalizeTaskWrites(tx); err != nil {
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
		if s.taskScheduler != nil {
			// Storage already validated and adopted the immutable candidate. The
			// Session owns its sole writer; scheduler bookkeeping reads this private
			// view only. Do not replay/validate the entire history a second time.
			<-s.core.line
			adopted := s.core.state
			s.core.leave()
			tx.taskAdoptState = &adopted
		}
		s.adoptTaskEffects(seq, tx)
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
func (s *Session) finalizeTaskWrites(tx *Tx) error {
	if s.taskScheduler != nil {
		if err := s.taskScheduler.prepareTaskWrites(tx); err != nil {
			return err
		}
	}
	return tx.finalizeTaskWrites()
}
func (s *Session) adoptTaskEffects(seq uint64, tx *Tx) {
	if s.taskScheduler != nil {
		s.taskScheduler.adopt(seq, tx)
	}
}

// taskBookkeeping holds the line without any store snapshot. Poison cannot
// prevent reservation rollback or Close from seeing every retained host join.
func (s *Session) taskBookkeeping(work func()) { <-s.line; defer s.leave(); work() }

// readTasks runs only bounded internal bookkeeping on the line. It is not a
// host callback API and never executes phases/migrations/provider effects.
func (s *Session) readTasks(ctx context.Context, read func(Snapshot) error) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	state, err := s.taskSnapshot(ctx)
	if err != nil {
		return err
	}
	return read(state)
}

// taskSnapshot is an immutable internal view while the Session line is owned.
// Tx methods detach values before exposing them; scheduler reads never expose
// these maps. Public Snapshot remains deeply detached. Preserve wrapped storage
// adapters (including fault gates) rather than bypassing their Snapshot contract.
func (s *Session) taskSnapshot(ctx context.Context) (Snapshot, error) {
	switch s.store.(type) {
	case *MemoryStorage, *JournalStorage:
		if err := s.core.enter(ctx); err != nil {
			return Snapshot{}, err
		}
		defer s.core.leave()
		return s.core.state, nil
	default:
		return s.store.Snapshot(ctx)
	}
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
