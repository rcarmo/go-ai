package durable

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The baseline used immediate Runtime.Commit. Only this adapter changed after
// recording its six failing backend leaves; assertions and callbacks are intact.
func mainRuntimeProgress(r *TaskRuntime, ctx context.Context, change func(*Tx, TaskRecord) (*TaskState, error)) error {
	return r.commitProgress(ctx, change)
}

// Source-derived Runtime PROGRESS_COMMIT prerequisites; these are not complete
// harness-deferred-progress bodies. Exercise a real native task and scheduler.
func TestMainHarnessProgressLaneMemoryAndFlush(t *testing.T) {
	for _, mode := range []string{"timer", "close", "failure"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				started := make(chan *TaskRuntime, 1)
				advance := make(chan struct{})
				result := make(chan error, 1)
				finish := make(chan struct{})
				var once sync.Once
				release := func() { once.Do(func() { close(finish) }) }
				defer release()
				defDoc := mustDefinition(t, DefinitionOptions{Kind: "test.progress-lane", Version: 1, Scope: "conversation", History: "latest", Fork: "initial", Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
				var owner ID
				def := taskDefinition(t, "test.progress-lane-task", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					started <- r
					select {
					case <-advance:
					case <-ctx.Done():
						return ctx.Err()
					}
					result <- mainRuntimeProgress(r, ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						doc, err := tx.AcquireDocument(defDoc, owner, nil, nil)
						if err != nil {
							return nil, err
						}
						return nil, doc.Update(func(v JSON) error { v["text"] = "held"; return nil })
					})
					select {
					case <-finish:
					case <-ctx.Done():
						return ctx.Err()
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				window := math.Inf(1)
				if mode != "close" {
					window = 400
				}
				var clock atomic.Int64
				clock.Store(1000)
				timer := make(chan func(), 4)
				options := Options{Settings: &HarnessSettings{Progress: &ProgressSettings{PersistWithinMs: &window}}, Now: clock.Load, SetTimer: func(callback func(), delay time.Duration, opts TimerOptions) func() {
					if !opts.Background && delay == 400*time.Millisecond {
						timer <- callback
					}
					return func() {}
				}}
				var h *Harness
				reports := make(chan error, 1)
				options.OnReportWithInfo = func(ctx context.Context, _ error, info ReportInfo) {
					if info.Type == "session_failed" {
						_, err := h.session.Snapshot(ctx)
						reports <- err
					}
				}
				h = taskTestHarnessOptions(t, b.store, options, def)
				c, err := h.Root(bg, AgentChange{})
				if err != nil {
					t.Fatal(err)
				}
				owner = c.ID()
				var document ID
				_, err = h.session.Commit(bg, func(tx *Tx) error {
					doc, err := tx.AcquireDocument(defDoc, owner, nil, nil)
					if err == nil {
						document = doc.id
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				watch, found, err := h.session.WatchDefinition(bg, defDoc, owner, nil)
				if err != nil || !found {
					t.Fatal(found, err)
				}
				deliveries := make(chan WatchFrame, 4)
				if err := watch.StartFrame(func(_ context.Context, f WatchFrame) error { deliveries <- f; return nil }); err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				id := createPublicTask(t, h, def, nil, TaskOptions{Conversation: owner, Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				runtime := watchReceive(t, started)
				before, err := h.session.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				h.session.taskBookkeeping(func() { h.scheduler.currentIdleIndex() })
				close(advance)
				if err := watchReceive(t, result); err != nil {
					t.Fatal(err)
				}
				frame := watchReceive(t, deliveries)
				memory, err := h.session.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := b.store.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Value["text"] != "held" || memory.Documents[document].Value["text"] != "held" {
					t.Fatal("memory delivery missing", frame.Value)
				}
				if stored.Documents[document].Value["text"] != "" || memory.Seq != before.Seq {
					t.Errorf("progress stored immediately: stored=%v seq=%d before=%d", stored.Documents[document].Value, memory.Seq, before.Seq)
				}
				if !equalJSONValue(before.Tasks[id], memory.Tasks[id]) {
					t.Error("document progress changed task checkpoint/state")
				}
				// Memory progress must not end a running task or leak its invocation permit.
				if runtime.ended.Load() {
					t.Error("memory progress ended invocation")
				}
				h.session.taskBookkeeping(func() {
					if h.scheduler.idleIndex == nil || !equalJSONValue(h.scheduler.idleIndex.image.Documents[document].Value, memory.Documents[document].Value) {
						t.Error("scheduler retained stale idle memory image")
					}
				})
				fault := errors.New("rollback progress")
				if err := mainRuntimeProgress(runtime, bg, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					doc, err := tx.AcquireDocument(defDoc, owner, nil, nil)
					if err != nil {
						return nil, err
					}
					if err := doc.Update(func(v JSON) error { v["text"] = "lost"; return nil }); err != nil {
						return nil, err
					}
					return nil, fault
				}); !errors.Is(err, fault) {
					t.Fatal("callback failure", err)
				}
				afterRollback, err := h.session.Snapshot(bg)
				if err != nil || afterRollback.Documents[document].Value["text"] != "held" {
					t.Fatal("callback failure changed held state", err)
				}
				ctx, cancel := context.WithCancel(bg)
				waiting := make(chan error, 1)
				go func() { _, err := h.WaitForTask(ctx, id); waiting <- err }()
				cancel()
				if err := watchReceive(t, waiting); !errors.Is(err, context.Canceled) {
					t.Fatal("waiter cancellation", err)
				}
				if mode == "failure" {
					fault := errors.New("persistence write failed")
					installTaskAppend(t, h, h.session.core, func(byte, uint64, uint64, []byte) error { return fault })
					failedWait := make(chan error, 1)
					go func() { _, err := h.WaitForTask(bg, id); failedWait <- err }()
					clock.Store(1400)
					fire := watchReceive(t, timer)
					fire()
					_, err := h.session.Snapshot(bg)
					var failure *SessionFailed
					// A lower-level append fault is an uncertain native settlement;
					// retain that first adopted poison identity across every guard.
					if !errors.As(err, &failure) || !errors.Is(err, ErrPoisoned) {
						t.Fatal("timer uncertain failure", err)
					}
					for _, err := range []error{watchReceive(t, failedWait), watchReceive(t, reports)} {
						var guarded *SessionFailed
						if !errors.As(err, &guarded) || guarded.Cause != failure.Cause {
							t.Fatal("wait/report failure identity", err)
						}
					}
					ctx, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					end, err := h.session.Closed(ctx)
					var endFailure *SessionFailed
					if err != nil || end.Reason != "failed" || !errors.As(end.Error, &endFailure) || endFailure.Cause != failure.Cause {
						t.Fatal("failed ending", end, err)
					}
					if h.session.core.state.Documents[document].Value["text"] != "" {
						t.Error("failed Session stored its held value")
					}
					return
				}
				if mode == "timer" {
					clock.Store(1400)
					select {
					case fire := <-timer:
						fire()
					default:
						t.Error("missing persistence timer")
					}
					if _, err := h.session.Snapshot(bg); err != nil {
						t.Fatal(err)
					}
					stored, err = b.store.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if stored.Documents[document].Value["text"] != "held" {
						t.Error("timer did not store held document")
					}
				}
				release()
				if _, err := h.WaitForTask(bg, id); err != nil {
					t.Fatal(err)
				}
				if err := h.WaitForIdle(bg); err != nil {
					t.Fatal(err)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				if h.session.core.state.Documents[document].Value["text"] != "held" {
					t.Error("healthy Close lost held document")
				}
				select {
				case f := <-deliveries:
					t.Error("storage replayed memory watch", f.Value)
				default:
				}
			})
		})
	}
}

func TestMainHarnessProgressLaneLifetimeFences(t *testing.T) {
	for _, mode := range []string{"ended", "aborted", "cancelled", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				started := make(chan *TaskRuntime, 1)
				result := make(chan error, 1)
				var called atomic.Bool
				def := taskDefinition(t, "test.progress-fences", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					started <- r
					switch mode {
					case "ended":
						if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil }); err != nil {
							return err
						}
					case "aborted":
						<-ctx.Done()
					}
					writeCtx := bg
					if mode == "cancelled" {
						var cancel context.CancelFunc
						writeCtx, cancel = context.WithCancel(bg)
						cancel()
					}
					err := mainRuntimeProgress(r, writeCtx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						called.Store(true)
						return taskDone(nil), nil // A task transition is always an immediate commit.
					})
					result <- err
					if mode == "cancelled" {
						return r.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
					}
					return nil
				})
				h := taskTestHarness(t, b.store, def)
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				watchReceive(t, started)
				aborted := make(chan error, 1)
				if mode == "aborted" {
					go func() { _, err := h.AbortTask(bg, id); aborted <- err }()
				}
				err := watchReceive(t, result)
				if mode == "mixed" {
					if err != nil || !called.Load() {
						t.Fatal("mixed transition", err)
					}
					stored, err := b.store.Snapshot(bg)
					if err != nil || !terminalStatus(stored.Tasks[id].Status) {
						t.Fatal("task transition was deferred", err)
					}
				} else {
					if err == nil || called.Load() {
						t.Fatal("progress escaped lifetime/cancellation fence", err, called.Load())
					}
					if mode == "cancelled" && !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
					if mode == "ended" && !errors.Is(err, ErrSealed) {
						t.Fatal(err)
					}
				}
				if mode == "aborted" {
					if err := watchReceive(t, aborted); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := h.WaitForTask(bg, id); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
