package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
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
			ch <- terminal("second answer")
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
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
				statusFailed140(t, write, "queued", "")
				close(release)
				failed := waitSubmission(t, first)
				if failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" {
					t.Fatalf("failed input settlement: %+v", failed.Record)
				}
				idleFailed140(t, h)
				statusFailed140(t, first, "unanswered", "model_error")
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				mainRequireRunFailure(t, state, c.ID(), failed.Task.ID, first.ID(), failed.Record.Entry, "model_error", "Model response ended with stop reason error", policy == "" || policy == "keep")
				switch policy {
				case "continue":
					settled := waitSubmission(t, queued).Record
					if settled.Status != "done" || settled.Type != "input" || settled.Answer == 0 {
						t.Fatalf("continued input settlement: %+v", settled)
					}
					answer, found, err := h.Entry(bg, settled.Answer)
					if err != nil || !found || len(answer.Model) != 1 || len(answer.Model[0].Content) != 1 || answer.Model[0].Content[0].Text != "second answer" {
						t.Fatalf("continued answer: %+v, found %t, err %v", answer, found, err)
					}
					statusFailed140(t, write, "done", "")
					if calls.Load() != 2 {
						t.Fatal(calls.Load())
					}
				case "withdraw":
					if settled := waitSubmission(t, queued).Record; settled.Status != "unanswered" || settled.Reason != "run_failed" || settled.Entry != 0 || settled.Answer != 0 {
						t.Fatalf("withdrawn input settlement: %+v", settled)
					}
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
					if settled := waitSubmission(t, fresh).Record; settled.Status != "done" {
						t.Fatalf("fresh input settlement: %+v", settled)
					}
					if settled := waitSubmission(t, queued).Record; settled.Status != "done" {
						t.Fatalf("kept input settlement: %+v", settled)
					}
					idleFailed140(t, h)
					statusFailed140(t, queued, "done", "")
					statusFailed140(t, write, "done", "")
				}
				if policy == "continue" || policy == "withdraw" {
					fresh, err := c.Submit(bg, Input{Content: "fresh"})
					if err != nil {
						t.Fatal(err)
					}
					waitSubmission(t, fresh)
					idleFailed140(t, h)
				}
				state, err = h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, doc := range state.Documents {
					if doc.Kind == "pi.live" && doc.Owner == c.ID() && doc.Value["runFailure"] != nil {
						t.Error("fresh run retained previous failure", doc.Value)
					}
				}
			})
		})
	}
}

func TestFailedRun140BoundedWithdrawalRecovery(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxWrites = 16
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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

// Canonical queued inputs have no task to own children. Attach the join witness
// to the actual failing generation, while its provider attempt is still live.
func TestSourceV160FailedRunWithdrawsBeforeOwnedHostReturns(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ref, options, calls, providerEntered, releaseProvider := failedRunProvider140(t)
		options.Settings = &HarnessSettings{AfterFailedRun: "withdraw"}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, providerEntered)
		queued, err := c.Submit(bg, Input{Content: "queued", WhenBusy: "followUp"})
		if err != nil {
			t.Fatal(err)
		}
		inspection, err := h.InspectTasks(bg)
		if err != nil || len(inspection.Tasks) != 1 || inspection.Tasks[0].Record.Kind != "pi.generation" {
			t.Fatalf("one active generation and no queued generation: %+v, err %v", inspection, err)
		}
		owner := inspection.Tasks[0].Record.ID
		started, cancelled, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, returned)
		def := taskDefinition(t, "task.failed-canonical-owned", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-returned
			return ctx.Err()
		})
		if _, err = h.options.Registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		child := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
		awaitTaskSignal(t, started)
		releaseTaskGate(releaseProvider)
		awaitTaskSignal(t, cancelled)
		if settled := waitSubmission(t, first).Record; settled.Status != "unanswered" || settled.Reason != "model_error" {
			t.Fatalf("failing run input: %+v", settled)
		}
		if settled := waitSubmission(t, queued).Record; settled.Status != "unanswered" || settled.Reason != "run_failed" || settled.Entry != 0 || settled.Answer != 0 {
			t.Fatalf("withdrawn input: %+v", settled)
		}
		held, found, err := h.Task(bg, owner)
		if err != nil || !found || held.State.Status != "completing" || held.State.Outcome == nil || held.State.Outcome.Status != "failed" {
			t.Fatalf("owner must stay completing until actual child return: %+v, found %t, err %v", held, found, err)
		}
		releaseTaskGate(returned)
		childResult := waitPublicTask(t, h, child)
		ownerResult := waitPublicTask(t, h, owner)
		if childResult.State.Outcome.Status != "aborted" || ownerResult.State.Outcome.Status != "failed" || calls.Load() != 1 {
			t.Fatalf("joined outcomes: child %+v owner %+v provider calls %d", childResult, ownerResult, calls.Load())
		}
	})
}

// Source harness-failed-run-inbox.test.ts:56. Unlike the combined policy
// fixture, there is no queued input that could start a second run here.
func TestSourceV160FailedRunWriteOnlyBoundary(t *testing.T) {
	for _, policy := range []string{"continue", "withdraw"} {
		t.Run(policy, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				ref, options, calls, entered, release := failedRunProvider140(t)
				options.Settings = &HarnessSettings{AfterFailedRun: policy}
				h := openHarness(t, b.store, options)
				root := root(t, h, ref)
				first, err := root.Submit(bg, Input{Content: "first"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				write, err := root.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "test.note", Model: []MessageReceipt{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "a note the model sees"}}, Timestamp: 1}}}})
				if err != nil {
					t.Fatal(err)
				}
				statusFailed140(t, write, "queued", "")
				releaseTaskGate(release)
				waitSubmission(t, first)
				idleFailed140(t, h)
				want := "queued"
				if policy == "continue" {
					want = "done"
				}
				statusFailed140(t, write, want, "")
				if calls.Load() != 1 {
					t.Fatalf("write-only boundary created another run: %d provider calls", calls.Load())
				}
			})
		})
	}
}
