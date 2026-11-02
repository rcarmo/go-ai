package durable

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Session serializes transaction preparation, storage settlement and committed
// reads. M1a has no execution/scheduling API and dispatches no external effects.
type Session struct {
	store                 Storage
	failureMu             sync.Mutex
	failureCause          error
	now                   func() int64 // line-owned wall clock for lifecycle stamps
	core                  *storeCore
	readImage             Snapshot // immutable receipted image; owned by the Session line
	heldDocuments         map[ID]heldDocument
	memoryImage           *Snapshot // immutable document overlay, never a durable receipt
	persistWithinMs       func() float64
	persistenceTimer      *sessionTimer
	persistenceReady      chan struct{} // stored-only publication released with the line
	line                  chan struct{}
	closing               atomic.Bool
	once                  sync.Once
	done                  chan struct{}
	closeErr              error
	limits                Limits
	contextRanges         map[ID]*contextRange
	contextRevision       uint64 // line-owned retention/host/settings witness for off-line reads
	contextExpiry         *sessionTimer
	contextExpiryAt       contextDeadline
	definitionCache       map[definitionCacheKey]definitionCacheValue
	definitionCacheBytes  int64
	documentRetentionMs   func() int64
	documentExpiry        *sessionTimer
	setTimer              SetTimer
	timerReport           func(error)
	clockReport           func(error)
	clockFallbackReported atomic.Bool
	pendingClockFailure   error // Session-line owned; delivered by leave
	timerFallbackReported atomic.Bool
	pendingTimers         [3]sessionTimerChange // line-owned fixed slots
	timerDispatch         sync.WaitGroup
	observerMu            sync.Mutex
	watches               map[*DocumentWatch]bool
	subscriptions         map[*CommitSubscription]bool
	syncCommitObservers   []*sessionCommitObservation // observerMu; insertion order
	localCommitListeners  []*sessionCommitListener    // observerMu; copied Set order
	taskScheduler         *taskScheduler              // private line-owned Harness integration
}

// OpenSession claims one native memory/journal store. Storage remains owned
// until the common Close operation drains admitted transactions and settles.
func OpenSession(store Storage) (*Session, error) {
	return OpenSessionWithOptions(store, SessionOptions{})
}

// SessionOptions.Now supplies Unix milliseconds; omitted uses the wall clock.
// It runs synchronously on the transaction line and must not reenter the Session.
// A panic uses the wall clock and reports once, off-line after line release.
type SessionOptions struct {
	Now                 func() int64
	DocumentRetentionMs *int64 // default600000; zero drops idle migrated values
	// SetTimer may decline background cache expiry; commits/reads still sweep.
	// Host functions run off-line and must not block or call Close (which joins them).
	SetTimer SetTimer
	OnReport func(error) // timer/clock host failures; observer/storage reporting is separate
}

func OpenSessionWithOptions(store Storage, options SessionOptions) (*Session, error) {
	if options.DocumentRetentionMs != nil && *options.DocumentRetentionMs < 0 {
		return nil, reject("negative document retention")
	}
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
	now := options.Now
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	if err := core.enter(context.Background()); err != nil {
		return nil, err
	}
	image := core.state
	core.leave()
	s := &Session{store: store, core: core, readImage: image, now: now, line: make(chan struct{}, 1), done: make(chan struct{}), limits: l, setTimer: options.SetTimer, timerReport: options.OnReport, clockReport: options.OnReport}
	if options.Now != nil {
		s.now = func() int64 {
			value, err := callClock(options.Now)
			if err != nil && s.clockFallbackReported.CompareAndSwap(false, true) {
				s.pendingClockFailure = err
			}
			return value
		}
	}
	retention := int64(600000)
	if options.DocumentRetentionMs != nil {
		retention = *options.DocumentRetentionMs
	}
	s.documentRetentionMs = func() int64 { return retention }
	s.line <- struct{}{}
	return s, nil
}
func (s *Session) enter(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	if err := s.failure(); err != nil {
		return err
	}
	if s.closing.Load() {
		return ErrClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.line:
	}
	if err := s.failure(); err != nil {
		s.leave()
		return err
	}
	if s.closing.Load() {
		s.leave()
		return ErrClosed
	}
	if e := ctx.Err(); e != nil {
		s.leave()
		return e
	}
	// A fired persistence timer was queued before this admission. Drain its
	// stored-only job first, even if its nonblocking host worker has not run yet.
	if timer := s.persistenceTimer; timer != nil && timer.fired.Load() {
		if err := s.flushFiredPersistence(); err != nil {
			s.leave()
			return err
		}
		// Only maintenance can have waited since the ordinary admission check.
		// Preserve the existing Context.Err call boundary for all other reads.
		if err := ctx.Err(); err != nil {
			s.leave()
			return err
		}
	}
	return nil
}
func (s *Session) leave() { s.leaveThen(nil) }
func (s *Session) leaveThen(afterRelease func()) {
	changes := s.pendingTimers
	s.pendingTimers = [3]sessionTimerChange{}
	persistenceReady := s.persistenceReady
	s.persistenceReady = nil
	clockFailure := s.pendingClockFailure
	s.pendingClockFailure = nil
	hasChanges := clockFailure != nil
	for _, change := range changes {
		if change.arm != nil {
			change.arm.started.Store(true)
		}
		hasChanges = hasChanges || change.arm != nil || change.stop != nil
	}
	// Admit before releasing the line: Close drains every already-admitted
	// arm/cancel, including a host arm still returning its cancellation function.
	if hasChanges {
		s.timerDispatch.Add(1)
	}
	s.line <- struct{}{}
	if hasChanges {
		defer s.timerDispatch.Done()
	}
	if persistenceReady != nil {
		close(persistenceReady)
		s.kickWatches()
	}
	if afterRelease != nil {
		afterRelease()
	}
	if clockFailure != nil {
		reportTaskError(s.clockReport, clockFailure)
	}
	if hasChanges {
		for _, change := range changes {
			if change.stop != nil {
				change.stop.disarm()
			}
			if change.arm != nil {
				change.arm.arm()
			}
		}
	}
}
func (s *Session) Snapshot(ctx context.Context) (Snapshot, error) {
	if e := s.enter(ctx); e != nil {
		return Snapshot{}, e
	}
	defer s.leave()
	return s.guardedStorage().Snapshot(ctx)
}
func (s *Session) MintID(ctx context.Context) (ID, error) {
	if e := s.enter(ctx); e != nil {
		return 0, e
	}
	defer s.leave()
	id, err := s.core.mintID(ctx, true)
	return id, s.backendResult(ctx, err, true)
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
func (s *Session) commit(ctx context.Context, callback func(*Tx) error, external bool, taskID ID) (uint64, error) {
	return s.commitPersistence(ctx, callback, external, taskID, false)
}

func (s *Session) commitPersistence(ctx context.Context, callback func(*Tx) error, external bool, taskID ID, deferred bool, requestedWindow ...float64) (seq uint64, err error) {
	if callback == nil {
		return 0, reject("nil transaction callback")
	}
	if err = s.enter(ctx); err != nil {
		return 0, err
	}
	if deferred && (!s.hasNativeReadImage() || (s.taskScheduler != nil && taskID == 0)) {
		s.leave()
		return 0, ErrUnsupported
	}
	var withinMs float64
	if deferred {
		withinMs, err = s.persistenceWindow(requestedWindow)
		if err != nil {
			s.leave()
			return 0, err
		}
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
	var telemetryEvents *durableEventBatch
	defer func() {
		if err != nil && tx != nil && tx.taskRollback != nil {
			tx.taskRollback(err)
		}
		if s.taskScheduler != nil && err != nil && errors.Is(err, ErrPoisoned) {
			s.taskScheduler.h.wakeAllSubmissionWaiters()
			s.taskScheduler.notifyWaiters(err)
			for target := range s.taskScheduler.tickets {
				s.taskScheduler.failTicket(target, ErrPoisoned)
			}
		}
		s.leaveThen(func() {
			close(ready)
			s.kickWatches()
			if s.taskScheduler != nil {
				s.taskScheduler.dispatchStops()
			}
		})
		if telemetryEvents != nil {
			if buffer := eventBufferFromContext(ctx); buffer != nil {
				buffer.enqueue(telemetryEvents)
			} else {
				telemetryEvents.deliver()
			}
		}
	}()
	state, e := s.taskSnapshot(ctx)
	if e != nil {
		err = e
		return 0, e
	}
	tx = &Tx{store: s.core, ctx: ctx, limits: s.limits, state: state, session: s, externalTaskCommit: external, progressTask: taskID}
	defer tx.seal()
	if err = callback(tx); err != nil {
		return 0, err
	}
	if err = s.failure(); err != nil {
		return 0, err
	}
	if tx.creationError != nil {
		return 0, tx.creationError
	}
	if state.Seq != s.readImage.Seq {
		// A typed version reload can independently store held changes while
		// this callback runs. Its rebased Tx owns the new receipt/history.
		state = tx.state
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
	if err = s.failure(); err != nil {
		return 0, err
	}
	if len(tx.writes) == 0 {
		return state.Seq, nil
	}
	tx.memoryOnly = deferred && s.canHoldDocuments(tx)
	prepared, e := s.prepareSessionPublication(ctx, tx, state, ready)
	if e != nil {
		return 0, e
	}
	if tx.memoryOnly {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := s.holdPreparedDocuments(&prepared, withinMs); err != nil {
			return 0, err
		}
		return state.Seq, nil
	}
	receipt, adopted, err := s.settlePreparedPublication(&prepared)
	seq = receipt.seq
	err = s.backendResult(ctx, err, true)
	if err != nil {
		return seq, err
	}
	telemetryEvents, err = s.publishSessionPublication(&prepared, receipt, &adopted)
	if err != nil {
		return 0, s.backendResult(ctx, err, true)
	}
	return seq, nil
}

func (s *Session) finalizeTaskWrites(tx *Tx) error {
	if s.taskScheduler != nil {
		if err := s.taskScheduler.prepareTaskWrites(tx); err != nil {
			return err
		}
	}
	if err := tx.syncGenerationStatuses(); err != nil {
		return err
	}
	if err := tx.syncCompactionStatuses(); err != nil {
		return err
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
	return s.readTaskView(ctx, func(state Snapshot, _ bool) error { return read(state) })
}

// The witness is captured by the read path, never inferred from Snapshot fields.
func (s *Session) readTaskView(ctx context.Context, read func(Snapshot, bool) error) error {
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	state, published, err := s.taskSnapshotView(ctx)
	if err != nil {
		return err
	}
	return read(state, published)
}

// hasNativeReadImage recognises only concrete native stores. Adapters remain
// authoritative, including replacements installed on the Session line after Open.
func (s *Session) hasNativeReadImage() bool {
	switch s.store.(type) {
	case *MemoryStorage, *JournalStorage:
		return true
	default:
		return directSQLiteTaskSnapshot(s.store)
	}
}

// taskSnapshot is an internal view captured while the Session line is owned.
// Native published payloads are immutable and may be retained by private off-line
// readers with explicit installation witnesses. Public Snapshot/scans detach
// values; scheduler reads never expose maps. Wrappers remain authoritative.
func (s *Session) taskSnapshot(ctx context.Context) (Snapshot, error) {
	state, _, err := s.taskSnapshotView(ctx)
	return state, err
}

func (s *Session) taskSnapshotView(ctx context.Context) (Snapshot, bool, error) {
	if !s.hasNativeReadImage() {
		state, err := s.guardedStorage().Snapshot(ctx)
		return state, false, err
	}
	highWater, err := s.core.sessionReadAdmission(ctx)
	if err != nil {
		return Snapshot{}, false, s.backendResult(ctx, err, false)
	}
	image := s.memoryReadImage()
	// ID reservations are independent of publication and remain globally
	// monotonic. Do not import raw records or its commit sequence with them.
	image.HighWater = highWater
	return image, true, nil
}

// adoptReadImage accepts only an immutable, receipted next publication image.
// A future group publisher must supply each part, never the final raw image
// for an earlier receipt. Callers hold the Session line and poison on error.
func (s *Session) adoptReadImage(image Snapshot, receipt uint64) error {
	if s.readImage.Seq >= MaxID || receipt != s.readImage.Seq+1 || image.Seq != receipt {
		return reject("non-contiguous publication image")
	}
	s.readImage = image
	s.memoryImage = nil
	return nil
}

func (s *Session) Close(ctx context.Context) error {
	if ctx == nil {
		return reject("nil context")
	}
	s.once.Do(func() {
		s.closing.Store(true)
		s.closeWatches()
		go func() {
			<-s.line
			if s.failure() == nil {
				s.closeErr = s.flushHeldOnClose()
			}
			s.endStoredCommitObservers()
			s.clearLocalCommitListeners()
			if s.persistenceTimer != nil {
				s.persistenceTimer.Stop()
				s.persistenceTimer = nil
			}
			s.heldDocuments = nil
			s.memoryImage = nil
			if s.contextExpiry != nil {
				s.contextExpiry.Stop()
			}
			s.contextRanges = nil
			s.readImage = Snapshot{}
			if s.documentExpiry != nil {
				s.documentExpiry.Stop()
			}
			s.definitionCache = nil
			s.definitionCacheBytes = 0
			if closeErr := s.core.close(context.Background(), true); s.closeErr == nil {
				s.closeErr = closeErr
			}
			s.leave()
			// Closing prevents further expiry admission. Waiting off-line allows
			// host timer/cancel functions to make reentrant observational reads.
			s.timerDispatch.Wait()
			close(s.done)
		}()
	})
	select {
	case <-s.done:
		return s.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
