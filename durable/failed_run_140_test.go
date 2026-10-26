package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
	"time"
)

func failedRunProvider140(t *testing.T) (ModelRef, Options, *atomic.Int64, <-chan struct{}, chan struct{}) {
	t.Helper()
	var calls atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	cleanupTaskGates(t, release)
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if calls.Add(1) == 1 {
			close(entered)
			go func() {
				defer close(ch)
				select {
				case <-release:
					msg := terminal("failed").Message
					msg.StopReason = goai.StopReasonError
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg, Err: fmt.Errorf("model failed")}
				case <-ctx.Done():
				}
			}()
		} else {
			ch <- terminal("recovered")
			close(ch)
		}
		return ch
	})
	return ref, options, &calls, entered, release
}
func idleFailed140(t *testing.T, h *Harness) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if err := h.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}
func statusFailed140(t *testing.T, sub *SubmissionHandle, status, reason string) {
	t.Helper()
	got, err := sub.Status(bg)
	if err != nil || got.Status != status || reason != "" && got.Reason != reason {
		t.Fatalf("submission %+v, %v; want %s/%s", got, err, status, reason)
	}
}

func TestFailedRun140InboxPolicies(t *testing.T) {
	for _, policy := range []string{"", "keep", "continue", "withdraw"} {
		t.Run("policy-"+policy, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				ref, options, calls, entered, release := failedRunProvider140(t)
				options.Settings = &HarnessSettings{AfterFailedRun: policy}
				h := openHarness(t, b.store, options)
				c := root(t, h, ref)
				first, err := c.Submit(bg, Input{Content: "first", RequestID: "first"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				queued, err := c.Submit(bg, Input{Content: "queued", RequestID: "queued"})
				if err != nil {
					t.Fatal(err)
				}
				write, err := c.Submit(bg, Input{Type: "write", Content: "passive", RequestID: "write"})
				if err != nil {
					t.Fatal(err)
				}
				close(release)
				waitSubmission(t, first)
				idleFailed140(t, h)
				statusFailed140(t, first, "unanswered", "model_error")
				switch policy {
				case "continue":
					statusFailed140(t, queued, "done", "")
					statusFailed140(t, write, "done", "")
					if calls.Load() != 2 {
						t.Fatal(calls.Load())
					}
				case "withdraw":
					statusFailed140(t, queued, "unanswered", "run_failed")
					statusFailed140(t, write, "queued", "")
					if calls.Load() != 1 {
						t.Fatal(calls.Load())
					}
				default:
					statusFailed140(t, queued, "queued", "")
					statusFailed140(t, write, "queued", "")
					duplicate, err := c.Submit(bg, Input{Content: "queued", RequestID: "queued"})
					if err != nil || duplicate.ID() != queued.ID() {
						t.Fatal("deduplication", err)
					}
					idleFailed140(t, h)
					statusFailed140(t, queued, "queued", "")
					if calls.Load() != 1 {
						t.Fatal("dedup released kept queue", calls.Load())
					}
					fresh, err := c.Submit(bg, Input{Content: "fresh", WhenBusy: "reject"})
					if err != nil {
						t.Fatal("held queue is not active busy work", err)
					}
					waitSubmission(t, fresh)
					idleFailed140(t, h)
					statusFailed140(t, queued, "done", "")
					statusFailed140(t, write, "done", "")
				}
			})
		})
	}
}

func TestFailedRun140BoundedWithdrawalRecovery(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxWrites = 16
	backends(t, func(t *testing.T, b backend) {
		ref, options, calls, entered, release := failedRunProvider140(t)
		options.Settings = &HarnessSettings{AfterFailedRun: "withdraw"}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		queued := []ID{}
		for n := 0; n < 20; n++ {
			sub, err := c.Submit(bg, Input{Content: fmt.Sprint("queued ", n)})
			if err != nil {
				t.Fatal(err)
			}
			queued = append(queued, sub.ID())
		}
		write, err := c.Submit(bg, Input{Type: "write", Content: "write"})
		if err != nil {
			t.Fatal(err)
		}
		// Freeze dispatch after the real provider effect has started, then observe
		// its adopted held decision. No model effect is mocked by a helper finish.
		h.scheduler.enabled.Store(false)
		held := make(chan struct{}, 1)
		_, observer, err := h.session.SubscribeCommits(bg)
		if err != nil {
			t.Fatal(err)
		}
		if err = observer.Start(func(_ context.Context, frame PublicationFrame) error {
			for _, w := range frame.Publication.Tables {
				if w.Task != nil && w.Task.Status == "completing" && taskHasDecidedOutcome(*w.Task) {
					select {
					case held <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		close(release)
		awaitTaskSignal(t, held)
		observer.Stop()
		h.scheduler.sealed.Store(true)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
				t.Fatal(err)
			}
			if cp.Submission == first.ID() {
				if _, err := h.AbortTask(bg, task.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
		fresh, err := c.Submit(bg, Input{Content: "post-decision"})
		if err != nil {
			t.Fatal(err)
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		// Pinning wins over a new host setting; small commit limits cannot be raised
		// to hide an oversized queue cleanup. Fresh input lies above the pinned cut.
		options.Settings = &HarnessSettings{AfterFailedRun: "continue"}
		reopened := b.reopen()
		h2 := openHarness(t, reopened, options)
		first2, err := h2.Submission(bg, first.ID())
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, first2)
		statusFailed140(t, first2, "unanswered", "model_error") // cancellation cannot replace the decided failure
		for _, id := range queued {
			sub, err := h2.Submission(bg, id)
			if err != nil {
				t.Fatal(err)
			}
			statusFailed140(t, sub, "unanswered", "run_failed")
		}
		fresh2, err := h2.Submission(bg, fresh.ID())
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, fresh2)
		idleFailed140(t, h2)
		if calls.Load() != 2 {
			t.Fatal("replayed failure or withdrew fresh input", calls.Load())
		}
		write2, err := h2.Submission(bg, write.ID())
		if err != nil {
			t.Fatal(err)
		}
		statusFailed140(t, write2, "done", "")
	}, limits)
}

func TestFailedRun140NoModelLeavesWritesQueued(t *testing.T) {
	for _, policy := range []string{"keep", "withdraw", "continue"} {
		t.Run(policy, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, release := make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, release)
				var resolutions atomic.Int64
				options := Options{Settings: &HarnessSettings{AfterFailedRun: policy}, Models: func(goai.Provider, string) *goai.Model {
					if resolutions.Add(1) == 1 {
						close(entered)
						<-release
					}
					return nil
				}}
				h := openHarness(t, b.store, options)
				c := root(t, h, ModelRef{Provider: goai.ProviderOpenAI, ID: "missing"})
				first, err := c.Submit(bg, Input{Content: "first"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				write, err := c.Submit(bg, Input{Type: "write", Content: "write"})
				if err != nil {
					t.Fatal(err)
				}
				close(release)
				waitSubmission(t, first)
				idleFailed140(t, h)
				statusFailed140(t, first, "unanswered", "no_model")
				want := "queued"
				if policy == "continue" {
					want = "done"
				}
				statusFailed140(t, write, want, "")
			})
		})
	}
}

func TestFailedRun140SettingsValidation(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		if _, err := Open(bg, b.store, Options{Settings: &HarnessSettings{AfterFailedRun: "invalid"}}); err == nil {
			t.Fatal("invalid Open policy")
		}
		h := openHarness(t, b.store, Options{})
		if h.AfterFailedRun() != "keep" {
			t.Fatal(h.AfterFailedRun())
		}
		if err := h.SetSettings(bg, HarnessSettings{AfterFailedRun: "invalid"}); err == nil {
			t.Fatal("invalid live policy")
		}
		if h.AfterFailedRun() != "keep" {
			t.Fatal("invalid settings adopted")
		}
		if err := h.SetSettings(bg, HarnessSettings{AfterFailedRun: "withdraw"}); err != nil {
			t.Fatal(err)
		}
		if h.AfterFailedRun() != "withdraw" {
			t.Fatal(h.AfterFailedRun())
		}
	})
}

func TestFailedRun140WithdrawalJoinsOwnedHosts(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ref, options, calls, entered, release := failedRunProvider140(t)
		options.Settings = &HarnessSettings{AfterFailedRun: "withdraw"}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		queued, err := c.Submit(bg, Input{Content: "queued"})
		if err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var owner ID
		for _, task := range state.Tasks {
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
				t.Fatal(err)
			}
			if cp.Submission == queued.ID() {
				owner = task.ID
			}
		}
		started, cancelled, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, returned)
		def := taskDefinition(t, "task.failed-queue-owned", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-returned
			return ctx.Err()
		})
		if _, err = h.options.Registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		close(release)
		waitSubmission(t, first)
		awaitTaskSignal(t, cancelled)
		statusFailed140(t, queued, "unanswered", "run_failed")
		var targets []ID
		if err = h.session.readTasks(bg, func(state Snapshot) error { targets = h.scheduler.idleTargets(state, 0); return nil }); err != nil {
			t.Fatal(err)
		}
		if len(targets) == 0 {
			t.Fatal("withdrawal hid an actual owned host")
		}
		close(returned)
		idleFailed140(t, h)
		if calls.Load() != 1 {
			t.Fatal("withdrawn input dispatched", calls.Load())
		}
	})
}
