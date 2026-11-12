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
	failureAt             uint64          // failureMu-owned first failure boundary
	failureOwn            map[uint64]bool // source #ownOutcomes
	lastDelivered         uint64          // last fenced outcome
	failureCause          error           // Go error view of the first failure
	failureValue          *Failure        // the first failure value exactly as reported
	now                   func() int64    // line-owned wall clock for lifecycle stamps
	core                  *storeCore
	readImage             Snapshot // immutable receipted image; owned by the Session line
	heldDocuments         map[ID]heldDocument
	memoryImage           *Snapshot // immutable document overlay, never a durable receipt
	persistWithinMs       func() float64
	persistenceTimer      *sessionTimer
	persistenceReady      chan struct{}        // stored-only publication released with the line
	jobs                  *sessionLine         // upstream #tail: held across read gates
	jobHeld               bool                 // memory-line owned: current holder also owns jobs
	line                  *sessionLine         // admission-ordered job line
	outcomeSlots          chan struct{}        // MAX_OUTSTANDING started-but-undelivered jobs
	deliveryTail          <-chan struct{}      // #enqueue delivery fence
	jobOutcome            *sessionOutcome      // memory-line owned; nil during off-line publication
	nextJobIndex          uint64               // line-owned: jobs are numbered in admission order
	flushImages           map[uint64]*Snapshot // writer flush goroutine only: image at each receipt
	writer                *sessionWriter       // owns admitted writes from storage to publication
	currentJobHit         bool                 // line-owned: the running job waited at a gate
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
	commitObservers       []*CommitObservation  // observerMu; dispatch-time membership
	pendingPublicCommits  []pendingPublicCommit // line-owned host dispatch phase
	commitDispatchGate    chan struct{}         // line-owned; opens after host callbacks
	commitDispatchNeeded  atomic.Bool
	watches               map[*DocumentWatch]bool
	subscriptions         map[*CommitSubscription]bool
	subscriptionOrder     uint64                        // observerMu; immutable registration ordinals
	conversationMounts    map[ID]*conversationViewMount // observerMu; last-owner release
	syncCommitObservers   []*sessionCommitObservation   // observerMu; insertion order
	localCommitListeners  []*sessionCommitListener      // observerMu; copied Set order
	documentObservers     []*sessionDocumentObservation // observerMu; private standalone lane
	taskScheduler         *taskScheduler                // private line-owned Harness integration
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
	s := &Session{store: store, core: core, readImage: image, now: now, line: newSessionLine(), jobs: newSessionLine(), outcomeSlots: make(chan struct{}, 256), done: make(chan struct{}), limits: l, setTimer: options.SetTimer, timerReport: options.OnReport, clockReport: options.OnReport}
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
	s.writer = s.newSessionWriterFromSession()
	return s, nil
}
func (s *Session) enter(ctx context.Context) error { return s.enterJob(ctx, false) }

func (s *Session) enterJob(ctx context.Context, exempt bool) error {
	if ctx == nil {
		return reject("nil context")
	}
	if err := s.failure(); err != nil {
		return err
	}
	if s.closing.Load() {
		return ErrClosed
	}
	if err := s.jobs.acquire(ctx); err != nil {
		return err
	}
	// #tail waits for room before starting the next job. Keep admission order
	// here, but never hold memory while a prior outcome is still pending.
	select {
	case s.outcomeSlots <- struct{}{}:
	case <-ctx.Done():
		s.jobs.release()
		return contextError(ctx)
	}
	if err := s.line.acquire(ctx); err != nil {
		<-s.outcomeSlots
		s.jobs.release()
		return err
	}
	s.jobHeld = true
	s.currentJobHit = false
	s.jobOutcome = s.enqueueOutcome()
	s.jobOutcome.exempt = exempt
	if err := s.awaitCommitDispatch(ctx); err != nil {
		s.leave()
		return err
	}
	if err := s.failure(); err != nil {
		s.leave()
		return err
	}
	if s.closing.Load() {
		s.leave()
		return ErrClosed
	}
	if e := contextError(ctx); e != nil {
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
		if err := contextError(ctx); err != nil {
			s.leave()
			return err
		}
	}
	return nil
}
func (s *Session) leave() { s.leaveThen(nil) }
func (s *Session) leaveThen(afterRelease func()) {
	s.drainPublicCommits(false)
	outcome := s.jobOutcome
	s.jobOutcome = nil
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
	s.releaseLine()
	outcome.deliver()
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

// handoffLeave gives up the line without running the pending timer changes. Two paths
// need that. A commit hands its batch to the writer here, and upstream arms the hold
// timer in the after-store publication, so the changes belong to the publication's
// owner. A publication releases the line the same way: disarming here would run host
// code while the part's gate is still open, and a host cancel that reads the Session
// would gate on the publication that is calling it. Whoever holds the line next -
// normally the commit, after its outcome closes and before it returns - flushes them.
func (s *Session) releaseLine() {
	jobHeld := s.jobHeld
	s.jobHeld = false
	s.line.release()
	if jobHeld {
		s.jobs.release()
	}
}

func (s *Session) handoffLeave(afterRelease func()) {
	s.jobOutcome = nil
	s.releaseLine()
	if afterRelease != nil {
		afterRelease()
	}
}

func (s *Session) Snapshot(ctx context.Context) (snapshot Snapshot, err error) {
	if e := s.enter(ctx); e != nil {
		return Snapshot{}, e
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
		if err != nil {
			snapshot = Snapshot{}
		}
	}()
	if e := s.touchAll(ctx); e != nil {
		return Snapshot{}, e
	}
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
// canceled preparation may reject before admission; afterwards the writer
// completes adopt-or-poison off the admission line.
func (s *Session) Commit(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	return s.commit(ctx, callback, true, 0)
}

func (s *Session) commitInternal(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	return s.commitJob(ctx, callback, false, 0, false, true)
}

func (s *Session) taskCommit(ctx context.Context, callback func(*Tx) error) (uint64, error) {
	return s.commitInternal(ctx, func(tx *Tx) error {
		if err := tx.touchKeys(ALL_KEYS); err != nil {
			return err
		}
		return callback(tx)
	})
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

func (s *Session) commitPersistence(ctx context.Context, callback func(*Tx) error, external bool, taskID ID, deferred bool, requestedWindow ...float64) (uint64, error) {
	return s.commitJob(ctx, callback, external, taskID, deferred, false, requestedWindow...)
}

func (s *Session) commitJob(ctx context.Context, callback func(*Tx) error, external bool, taskID ID, deferred, exempt bool, requestedWindow ...float64) (seq uint64, err error) {
	if callback == nil {
		return 0, reject("nil transaction callback")
	}
	if err = s.enterJob(ctx, exempt); err != nil {
		return 0, err
	}
	outcome := s.jobOutcome
	defer func() { outcome.deliver(); err = s.deliveredError(outcome, err) }()
	// Scheduler callbacks still consume whole-state private bookkeeping. Public
	// Session transactions read through the Transaction host's dependency keys.
	keyedReads := s.hasNativeReadImage() && !deferred
	if !keyedReads || (!external && !exempt) {
		if err = s.touchAll(ctx); err != nil {
			s.leave()
			return 0, err
		}
	}
	// The owned store cannot have an untracked committed prefix after the
	// publication barrier. Check under its own line, before preparing writes.
	if s.hasNativeReadImage() && s.writer.drained() == nil {
		<-s.core.line
		gap := s.core.state.Seq != s.readImage.Seq
		s.core.leave()
		if gap {
			err = s.backendResult(ctx, reject("non-contiguous publication image"), true)
			s.leave()
			return 0, err
		}
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
	// The handoff gives the line up before the outcome is awaited, so teardown has
	// to work either way: still on the line, or after taking it back.
	lineHeld := true
	var readyClosed bool
	// Task authority is released before the job stops being visible on the line, so
	// a later line entrant can never observe a rejected provisional runtime. The
	// writer's discard hook owns that ordering once a commit hands the line off.
	var taskRolledBack bool
	rollbackTask := func(failure error) {
		if failure == nil || taskRolledBack || tx == nil || tx.taskRollback == nil {
			return
		}
		taskRolledBack = true
		tx.taskRollback(failure)
	}
	afterRelease := func() {
		if !readyClosed {
			readyClosed = true
			close(ready)
		}
		s.kickWatches()
		if s.taskScheduler != nil {
			s.taskScheduler.dispatchStops()
		}
	}
	defer func() {
		// Teardown is line-owned, so take the line back if the handoff gave it up.
		if !lineHeld && s.line.acquire(context.Background()) == nil {
			lineHeld = true
		}
		if err != nil {
			rollbackTask(err)
		}
		if s.taskScheduler != nil && err != nil && errors.Is(err, ErrPoisoned) {
			s.taskScheduler.h.wakeAllSubmissionWaiters()
			s.taskScheduler.notifyWaiters(err)
			for target := range s.taskScheduler.tickets {
				s.taskScheduler.failTicket(target, ErrPoisoned)
			}
		}
		if lineHeld {
			s.leaveThen(afterRelease)
		} else {
			afterRelease()
		}
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
	tx = &Tx{store: s.core, ctx: ctx, limits: s.limits, state: state, session: s, externalTaskCommit: external, progressTask: taskID, keyedReads: keyedReads}
	tx.identity = tx
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
	if err = contextError(ctx); err != nil {
		return 0, err
	}
	// Task owner validation and same-batch retirement precede document content
	// selection. This private sealed-Tx path executes no task/host callbacks.
	if err = s.finalizeTaskWrites(tx); err != nil {
		return 0, err
	}
	if err = tx.finalizeSubmissionWrites(); err != nil {
		return 0, err
	}
	if err = tx.finalizeDocuments(); err != nil {
		return 0, err
	}
	if err = contextError(ctx); err != nil {
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
		outcome.after = true
		if err := contextError(ctx); err != nil {
			return 0, err
		}
		if err := s.holdPreparedDocuments(&prepared, withinMs); err != nil {
			return 0, err
		}
		return state.Seq, nil
	}
	pub := &admittedPublication{}
	part, e := s.admitPreparedPublication(&prepared, outcome.index, pub, rollbackTask)
	if e != nil {
		return 0, e
	}
	// The handoff. From here the line is free while the writer stores this commit
	// and then publishes it; the outcome is awaited off the line.
	outcome.after = true
	lineHeld = false
	s.handoffLeave(afterRelease)
	<-part.outcome
	// Deliver before reentrant host timer flushes in teardown; a host cancel
	// may read the Session and must not await the job invoking it.
	outcome.deliver()
	telemetryEvents = pub.telemetry
	seq = pub.seq
	if part.err != nil {
		return seq, part.err
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
func (s *Session) taskBookkeeping(work func()) {
	_ = s.jobs.acquire(context.Background())
	if s.line.acquire(context.Background()) != nil {
		s.jobs.release()
		return
	}
	s.jobHeld = true
	defer s.leave()
	work()
}

// readWakeImage observes the scheduler's already-published graph. Upstream
// Scheduler.wake() is synchronous and does not touch unpublished storage keys.
// Maintenance acquires Go memory without adding an outcome fence or draining
// unrelated writes.
func (s *Session) readWakeImage(ctx context.Context, read func(Snapshot) error) error {
	if err := s.jobs.acquire(ctx); err != nil {
		return err
	}
	if err := s.line.acquire(ctx); err != nil {
		s.jobs.release()
		return err
	}
	s.jobHeld = true
	defer s.leave()
	if err := s.failure(); err != nil {
		return err
	}
	state, err := s.taskSnapshot(ctx)
	if err != nil {
		return err
	}
	return read(state)
}

// readTasks runs only bounded internal bookkeeping on the line. It is not a
// host callback API and never executes phases/migrations/provider effects.
func (s *Session) readTasks(ctx context.Context, read func(Snapshot) error) error {
	return s.readTaskView(ctx, func(state Snapshot, _ bool) error { return read(state) })
}

// The witness is captured by the read path, never inferred from Snapshot fields.
func (s *Session) readTaskView(ctx context.Context, read func(Snapshot, bool) error) (err error) {
	if err := s.enter(ctx); err != nil {
		return err
	}
	outcome := s.jobOutcome
	defer func() {
		s.leave()
		err = s.deliveredError(outcome, err)
	}()
	if err := s.touchAll(ctx); err != nil {
		return err
	}
	state, published, err := s.taskSnapshotView(ctx)
	if err != nil {
		return err
	}
	return read(state, published)
}

// hasNativeReadImage recognises only concrete native stores. Adapters remain
// authoritative, including replacements installed on the Session line after Open.
func (s *Session) hasNativeReadImage() bool {
	return directSQLiteTaskSnapshot(s.store)
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
			// Every commit admitted before the seal publishes before the line job that
			// clears the state its publication adopts into.
			if drained := s.writer.drained(); drained != nil {
				<-drained
			}
			if s.line.acquire(context.Background()) != nil {
				return
			}
			if s.failure() == nil {
				s.closeErr = s.flushHeldOnClose()
			}
			s.drainPublicCommits(true)
			s.endPublicCommitObservers()
			s.endStoredCommitObservers()
			s.endDocumentObservers()
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
		return contextError(ctx)
	}
}
