package durable

import (
	"context"
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
	if err := conversation.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
