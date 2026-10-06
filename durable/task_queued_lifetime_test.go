package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// The deciding commit owns the Session line before admission is witnessed.
// Queued capabilities must recheck invocation authority before reads, callbacks,
// scheduling or writes. This does not depend on Go select FIFO ordering.
func TestTaskRuntimeQueuedCapabilitySealAndCallerCancellation(t *testing.T) {
	operations := []string{"memo", "task", "outcomes", "entry", "typed-entry", "context", "document", "historical-document", "conversation", "conversation-agent", "submit", "abort", "abort-background", "idle", "task-wait", "agent", "environment", "hooks", "memo-candidate", "commit", "watch"}
	for _, operation := range operations {
		for _, disposition := range []string{"seal", "caller-cancel"} {
			t.Run(operation+"/"+disposition, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					entered, release := make(chan struct{}), make(chan struct{})
					var runtime *TaskRuntime
					owner := taskDefinition(t, "task.queued.capability", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
						runtime = r
						close(entered)
						<-release
						return nil
					})
					h := taskTestHarness(t, b.store, owner)
					cleanupTaskGates(t, release)
					var migrations, effects atomic.Int64
					document := mustDefinition(t, DefinitionOptions{Kind: "app.queued.capability", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{"value": 1}, nil }})
					migrated := mustDefinition(t, DefinitionOptions{Kind: document.options.Kind, Version: 2, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: document.options.Initial, Migrate: func(value JSON, _ uint64) (JSON, error) { migrations.Add(1); return value, nil }})
					token := entryToken(t, "app.queued.entry")
					var entry ID
					if _, err := h.Commit(bg, func(tx *Tx) error {
						if _, err := tx.AcquireDocument(document, 1, nil, nil); err != nil {
							return err
						}
						v, err := tx.AppendTypedEntry(token, 1, EntryContent{Data: JSON{"value": 1}, HasData: true})
						entry = v.ID
						return err
					}); err != nil {
						t.Fatal(err)
					}
					id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err := h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					awaitTaskSignal(t, entered)
					conversation, err := runtime.Conversation(bg, 1)
					if err != nil {
						t.Fatal(err)
					}
					caller, cancel := context.WithCancel(bg)
					defer cancel()
					queued := make(chan struct{})
					ctx := &taskAdmissionContext{Context: caller, queued: queued}
					result := make(chan error, 1)
					invoke := func() error {
						switch operation {
						case "memo":
							_, _, err := runtime.Memo(ctx, "missing")
							return err
						case "task":
							_, _, err := runtime.Task(ctx, id)
							return err
						case "outcomes":
							_, err := runtime.Outcomes(ctx, []ID{})
							return err
						case "entry":
							_, _, err := runtime.Entry(ctx, entry)
							return err
						case "typed-entry":
							_, _, err := runtime.TypedEntry(ctx, token, entry)
							return err
						case "context":
							_, err := runtime.ContextView(ctx, 1, 0)
							return err
						case "document":
							_, _, err := runtime.SnapshotDefinition(ctx, migrated, 1, nil)
							return err
						case "historical-document":
							_, _, err := runtime.SnapshotDefinitionAsOf(ctx, migrated, 1, nil, entry)
							return err
						case "conversation":
							_, err := runtime.Conversation(ctx, 1)
							return err
						case "conversation-agent":
							_, err := conversation.Agent(ctx)
							return err
						case "submit":
							_, err := conversation.Submit(ctx, Input{Content: "must not be admitted"})
							return err
						case "abort":
							return conversation.Abort(ctx)
						case "abort-background":
							return conversation.AbortWithOptions(ctx, ConversationAbortOptions{Background: true})
						case "idle":
							return conversation.WaitForIdle(ctx)
						case "task-wait":
							_, err := runtime.WaitForTask(ctx, id)
							return err
						case "agent":
							_, err := runtime.Agent(ctx)
							return err
						case "environment":
							_, err := runtime.Environment(ctx)
							return err
						case "hooks":
							return runtime.EachHook(ctx, "probe", func(TaskHook) error { effects.Add(1); return nil })
						case "memo-candidate":
							_, err := runtime.MemoCandidate(ctx, "must-not-write", JSON{})
							return err
						case "commit":
							return runtime.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { effects.Add(1); return nil, nil })
						case "watch":
							watch, err := runtime.WatchDefinition(ctx, migrated, 1, nil)
							if watch != nil {
								watch.Stop()
								effects.Add(1)
							}
							return err
						default:
							panic(operation)
						}
					}
					if err := runtime.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
						go func() { result <- invoke() }()
						awaitTaskSignal(t, queued)
						if disposition == "caller-cancel" {
							cancel()
							return nil, nil
						}
						return taskDone("sealed"), nil
					}); err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-result:
						expected := ErrSealed
						if disposition == "caller-cancel" {
							expected = context.Canceled
						}
						if !errors.Is(err, expected) && !(disposition == "seal" && errors.Is(err, context.Canceled)) {
							t.Fatalf("queued capability: want %v, got %v", expected, err)
						}
					case <-time.After(3 * time.Second):
						t.Fatal("queued capability retained after authority loss")
					}
					// The pinned watch path acquires first, then stops a late watch;
					// unlike #read, its baseline may hydrate before attachment seals.
					wantMigrations := int64(0)
					if operation == "watch" && disposition == "seal" {
						wantMigrations = 1
					}
					if migrations.Load() != wantMigrations || effects.Load() != 0 {
						t.Fatalf("queued capability callback count: migrations=%d want=%d effects=%d", migrations.Load(), wantMigrations, effects.Load())
					}
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if len(state.Submissions) != 0 {
						t.Fatal("queued submit admitted", state.Submissions)
					}
					h.session.taskBookkeeping(func() {
						if len(runtime.watches) != 0 || len(h.session.watches) != 0 || len(h.submissionWaiters) != 0 || len(h.scheduler.waiters) != 0 || len(h.scheduler.tickets) != 0 {
							t.Error("queued capability leaked registration/claim")
						}
					})
					if disposition == "caller-cancel" {
						if err := runtime.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("caller remains live"), nil }); err != nil {
							t.Fatal(err)
						}
					}
					releaseTaskGate(release)
				})
			})
		}
	}
}
