package durable

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source 81f3cd41 harness-idle-scopes: no-op expiry, clock rollback, zero
// retention and rereads. Shorter/longer retention are native boundary controls.
// Virtual timers never fire; these are expiry values, not indexed-work proofs.
func TestMainContextIdleExpiryBoundaries(t *testing.T) {
	for _, mode := range []string{"no-op", "backwards-order", "zero-backwards", "shorter", "longer", "reread"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				other, entry, otherEntry := mint(t, b.store), mint(t, b.store), mint(t, b.store)
				first := contextEntry(entry, 1, goai.RoleUser, "first")
				second := contextEntry(otherEntry, other, goai.RoleUser, "second")
				apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: other}}, Write{Op: "append-entry", Entry: &first}, Write{Op: "append-entry", Entry: &second})
				var clock atomic.Int64
				clock.Store(1000)
				if mode == "zero-backwards" {
					clock.Store(2000)
				}
				h := mainContextScopeHarness(t, b.store, &clock)
				read := func(id ID) {
					t.Helper()
					if _, err := h.session.ContextView(bg, id, 0); err != nil {
						t.Fatal(err)
					}
				}
				kept := func(id ID, want bool, since int64) {
					t.Helper()
					h.session.taskBookkeeping(func() {
						r := h.session.contextRanges[id]
						if (r != nil) != want {
							t.Fatalf("range %d retained=%v want=%v at clock=%d", id, r != nil, want, clock.Load())
						}
						if want && (r.idleSince == nil || *r.idleSince != since) {
							t.Fatal("idle stamp changed", r.idleSince, since)
						}
					})
				}
				retention := func(ms int64) {
					t.Helper()
					if err := h.SetSettings(bg, HarnessSettings{ContextRetentionMs: &ms}); err != nil {
						t.Fatal(err)
					}
				}
				reconcile := func() {
					t.Helper()
					before, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if err := h.scheduler.reconcile(); err != nil {
						t.Fatal(err)
					}
					after, err := h.Snapshot(bg)
					if err != nil || after.Seq != before.Seq {
						t.Fatal("no-op reconciliation wrote storage", before.Seq, after.Seq, err)
					}
				}
				read(1)
				switch mode {
				case "no-op":
					clock.Store(2500)
					reconcile()
					kept(1, false, 0)
				case "backwards-order":
					clock.Store(500)
					read(other)
					kept(1, true, 1000)
					kept(other, true, 500)
					clock.Store(1600)
					reconcile()
					kept(1, true, 1000)
					kept(other, false, 0)
				case "zero-backwards":
					clock.Store(1000)
					retention(0)
					kept(1, false, 0)
				case "shorter":
					clock.Store(1600)
					retention(500)
					kept(1, false, 0)
				case "longer":
					clock.Store(1500)
					retention(3000)
					kept(1, true, 1000)
					clock.Store(3500)
					reconcile()
					kept(1, true, 1000)
					clock.Store(4000)
					reconcile()
					kept(1, false, 0)
				case "reread":
					clock.Store(1600)
					read(1)
					kept(1, true, 1000)
					clock.Store(2000)
					reconcile()
					kept(1, false, 0)
				}
			})
		})
	}
}

func TestMainContextBusyDoesNotJoinUnpublishedWriter(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 1)
		var clock atomic.Int64
		clock.Store(1000)
		h := mainContextScopeHarness(t, b.store, &clock)
		held, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		var jobs sync.WaitGroup
		defer func() { unblock(); jobs.Wait() }()
		appendFrame := h.session.core.appendFrame
		h.session.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
			close(held)
			<-release
			if appendFrame != nil {
				return appendFrame(kind, ordinal, high, payload)
			}
			return nil
		}
		writer := make(chan error, 1)
		jobs.Add(1)
		go func() { defer jobs.Done(); _, err := h.session.core.commitParts(bg, batches, nil, true); writer <- err }()
		select {
		case <-held:
		case <-time.After(5 * time.Second):
			t.Fatal("writer never reached gate")
		}
		read := make(chan error, 1)
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			read <- h.session.readTasks(context.Background(), func(Snapshot) error {
				if h.session.contextBusy(1) {
					return storageRequest("unexpected busy scope")
				}
				return nil
			})
		}()
		select {
		case err := <-read:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("published context membership waited for raw writer")
		}
		unblock()
		if err := <-writer; err != nil {
			t.Fatal(err)
		}
		jobs.Wait()
	})
}

// Source harness-idle-scopes.test.ts:269. A sleep keeps an idle waiter
// blocked but lets the retained context age until runnable work appears.
func TestSourceV160ParkedScopeAgesContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 6*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(1000)
	retention := int64(1000)
	answer := testSignalAnswer(t)
	key, err := answer.Key("go")
	if err != nil {
		t.Fatal(err)
	}
	parked := make(chan error, 1)
	parker, err := DefineTask(TaskDefinitionOptions{Kind: "test.idle.parker", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
		Abort:   abortTestSignalTask,
		Phases: map[string]TaskPhase{
			"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				if _, err := r.ContextView(ctx, task.Conversation, 0); err != nil {
					parked <- err
					return err
				}
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "woke"}, Signals: []SignalKey{key}}, nil
				})
				parked <- err
				return err
			},
			"woke": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	runner := taskDefinition(t, "test.idle.running", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		close(started)
		select {
		case <-release:
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	h := taskTestHarnessOptions(t, store, Options{Now: clock.Load, Settings: &HarnessSettings{ContextRetentionMs: &retention}, SetTimer: func(func(), time.Duration, TimerOptions) func() { return func() {} }}, parker, runner)
	cleanupTaskGates(t, release)
	conversation, err := h.Root(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conversation.Submit(ctx, Input{Type: "write", Entry: &Entry{Kind: "test.note"}}); err != nil {
		t.Fatal(err)
	}
	id := createPublicTask(t, h, parker, nil, TaskOptions{Conversation: conversation.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-parked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cid := conversation.ID()
	if task, found, err := h.Task(ctx, id); err != nil || !found || task.State.Status != "sleeping" {
		t.Fatal("parked receipt", task, found, err)
	}
	idle := awaitPendingContextIdleWait(t, ctx, h, conversation)
	// Source records these via its scheduler idle-state debug symbol. A
	// committed task is still an idle target while its context has an age.
	h.session.taskBookkeeping(func() {
		r := h.session.contextRanges[cid]
		if r == nil || len(h.session.contextRanges) != 1 || r.idleSince == nil || *r.idleSince != 1000 {
			t.Fatalf("parked scope context is not aging: %+v", r)
		}
		if targets, _ := h.scheduler.indexedIdleTargets(cid); len(targets) != 1 || targets[0] != id {
			t.Fatalf("parked scope idle targets = %v; want [%d]", targets, id)
		}
	})
	running := createPublicTask(t, h, runner, nil, TaskOptions{Conversation: cid, Ownership: TaskOwnership{Kind: "conversation"}})
	awaitTaskSignal(t, started)
	h.session.taskBookkeeping(func() {
		r := h.session.contextRanges[cid]
		if r == nil || len(h.session.contextRanges) != 1 || r.idleSince != nil {
			t.Fatalf("running scope context still aging: %+v", r)
		}
	})
	releaseTaskGate(release)
	if _, err := h.WaitForTask(ctx, running); err != nil {
		t.Fatal(err)
	}
	if got, err := h.Signal(ctx, id, answer, key.Key, JSON{"text": "go"}); err != nil || got != SignalDelivered {
		t.Fatalf("wake parked scope = %q, %v", got, err)
	}
	if err := watchReceive(t, idle); err != nil {
		t.Fatal(err)
	}
}

// A Done-keyed receipt wakes the test as the public call reaches its
// blocking select. Check registration under the Session line instead of
// assuming a goroutine that has not returned yet has actually been admitted.
func awaitPendingContextIdleWait(t *testing.T, ctx context.Context, h *Harness, c *ConversationHandle) <-chan error {
	t.Helper()
	progress, result := make(chan struct{}, 16), make(chan error, 1)
	go func() { result <- c.WaitForIdle(submissionWaitContext{ctx, progress}) }()
	for {
		select {
		case err := <-result:
			t.Fatalf("public wait returned before registering for scope %d: %v", c.ID(), err)
		case <-ctx.Done():
			t.Fatalf("public wait did not register for scope %d: %v", c.ID(), ctx.Err())
		case <-progress:
			registered := false
			h.session.taskBookkeeping(func() {
				for waiter := range h.scheduler.waiters {
					if waiter.idleScope != nil && *waiter.idleScope == c.ID() {
						registered = true
					}
				}
			})
			if registered {
				select {
				case err := <-result:
					t.Fatalf("public wait settled while scope %d was still busy: %v", c.ID(), err)
				default:
					return result
				}
			}
		}
	}
}

// harness-idle-scopes.test.ts:252-265. Both public waits register while
// busy; completing only the unrelated task cannot release the first wait.
func TestSourceV160IdleWaitKeepsUnrelatedBusyScopePending(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	type taskGate struct{ started, release chan struct{} }
	held := taskGate{make(chan struct{}), make(chan struct{})}
	quick := taskGate{make(chan struct{}), make(chan struct{})}
	work := taskDefinition(t, "test.idle.separate-scopes", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
		gate := held
		if task.Input.Value == "quick" {
			gate = quick
		}
		close(gate.started)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-gate.release:
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		}
	})
	h := taskTestHarness(t, store, work)
	cleanupTaskGates(t, quick.release, held.release)
	busy, err := h.CreateConversation(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.CreateConversation(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	create := func(c *ConversationHandle, name string) ID {
		t.Helper()
		value, err := c.Commit(ctx, func(tx *Tx) (any, error) {
			return tx.CreateTask(work, name, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		})
		if err != nil {
			t.Fatal(err)
		}
		return value.(ID)
	}
	heldID, quickID := create(busy, "held"), create(other, "quick")
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, held.started)
	awaitTaskSignal(t, quick.started)
	idleBusy := awaitPendingContextIdleWait(t, ctx, h, busy)
	idleOther := awaitPendingContextIdleWait(t, ctx, h, other)
	close(quick.release)
	if got := waitPublicTask(t, h, quickID); got.State.Status != "terminal" || got.State.Outcome.Status != "completed" {
		t.Fatal("unrelated task did not complete", got)
	}
	select {
	case err := <-idleOther:
		if err != nil {
			t.Fatal("unrelated conversation did not become idle", err)
		}
	case <-ctx.Done():
		t.Fatalf("unrelated idle wait did not return while the first task was held: %v", ctx.Err())
	}
	if got, found, err := h.Task(ctx, heldID); err != nil || !found || got.State.Status != "running" {
		t.Fatal("held task must still be running", got, found, err)
	}
	h.session.taskBookkeeping(func() {
		busyWaits := 0
		for wait := range h.scheduler.waiters {
			if wait.idleScope != nil && *wait.idleScope == busy.ID() {
				busyWaits++
			}
		}
		if busyWaits != 1 {
			t.Fatalf("after unrelated completion: %d busy-scope waits, want one", busyWaits)
		}
	})
	select {
	case err := <-idleBusy:
		t.Fatalf("busy idle wait settled after unrelated task completion: %v", err)
	default:
	}
	close(held.release)
	if got := waitPublicTask(t, h, heldID); got.State.Status != "terminal" || got.State.Outcome.Status != "completed" {
		t.Fatal("held task did not complete after release", got)
	}
	if err := watchReceive(t, idleBusy); err != nil {
		t.Fatal("held conversation did not become idle", err)
	}
}

// harness-idle-scopes.test.ts:347-364. X's last context read is retained
// while a different conversation Z has one held task. An idle wait for Z
// must drive expiry without a timer callback or a durable state change.
func TestSourceV160IdleWaitExpiresContextWithoutCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	retention := int64(1000)
	armed, stopped := make(chan struct{}, 1), make(chan struct{}, 1)
	var scheduler atomic.Pointer[taskScheduler]
	var advanceAfter atomic.Uint64
	now := func() int64 {
		// The reader has joined and the second host stays held, so the
		// public wait advances the epoch. Old passes see the old time.
		if epoch := advanceAfter.Load(); epoch != 0 && scheduler.Load().epoch.Load() >= epoch {
			return 2500
		}
		return 1000
	}
	var xID ID
	readerJoined := make(chan (<-chan struct{}), 1)
	reader := taskDefinition(t, "test.idle.context-reader", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		if _, err := r.ContextView(ctx, xID, 0); err != nil {
			return err
		}
		readerJoined <- r.done
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	held, release := make(chan struct{}), make(chan struct{})
	holder := taskDefinition(t, "test.idle.context-holder", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		close(held)
		select {
		case <-release:
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	h := taskTestHarnessOptions(t, store, Options{Now: now, Settings: &HarnessSettings{ContextRetentionMs: &retention},
		SetTimer: func(_ func(), delay time.Duration, options TimerOptions) func() {
			if !options.Background || delay != time.Second {
				return nil
			}
			select {
			case armed <- struct{}{}:
			default:
			}
			// Never fire the supplied callback; the host receipt is only a
			// barrier for cancellation after the cache actually expires.
			return func() {
				select {
				case stopped <- struct{}{}:
				default:
				}
			}
		}}, reader, holder)
	scheduler.Store(h.scheduler)
	cleanupTaskGates(t, release)
	x, err := h.Root(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	xID = x.ID()
	if _, err := x.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.AppendEntry(x.ID(), EntryDraft{"kind": "note"})
	}); err != nil {
		t.Fatal(err)
	}
	z, err := h.CreateConversation(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	r := createPublicTask(t, h, reader, nil, TaskOptions{Conversation: z.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
	waitPublicTask(t, h, r)
	awaitTaskSignal(t, watchReceive(t, readerJoined))
	id := createPublicTask(t, h, holder, nil, TaskOptions{Conversation: z.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
	awaitTaskSignal(t, held)
	awaitTaskSignal(t, armed)
	before, found, err := h.Task(ctx, id)
	if err != nil || !found || before.State.Status != "running" {
		t.Fatal("held task", before, found, err)
	}
	head := readOnlyHarnessSQLiteHead(t, store)
	h.session.taskBookkeeping(func() {
		contexts, idleContexts := len(h.session.contextRanges), 0
		for _, retained := range h.session.contextRanges {
			if retained.idleSince != nil {
				idleContexts++
			}
		}
		rangeX := h.session.contextRanges[x.ID()]
		if contexts != 1 || idleContexts != 1 || rangeX == nil || rangeX.idleSince == nil || *rangeX.idleSince != 1000 {
			t.Fatalf("before idle review: %d contexts, %d idle; want one of each at age 1000", contexts, idleContexts)
		}
		advanceAfter.Store(h.scheduler.epoch.Load() + 1)
	})
	idle := awaitPendingContextIdleWait(t, ctx, h, z)
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatalf("no-op review did not expire the idle range: %v", ctx.Err())
	}
	h.session.taskBookkeeping(func() {
		contexts, idleMismatches := len(h.session.contextRanges), 0
		for conversation, retained := range h.session.contextRanges {
			if (retained.idleSince == nil) != h.session.contextBusy(conversation) {
				idleMismatches++
			}
		}
		if contexts != 0 || idleMismatches != 0 {
			t.Fatalf("after idle review: %d contexts, %d idle mismatches; want both zero", contexts, idleMismatches)
		}
	})
	after, found, err := h.Task(ctx, id)
	if err != nil || !found || !reflect.DeepEqual(before, after) || readOnlyHarnessSQLiteHead(t, store) != head {
		t.Fatal("idle review changed task or durable head", after, found, err)
	}
	releaseTaskGate(release)
	if err := watchReceive(t, idle); err != nil {
		t.Fatal(err)
	}
}
