package durable

import (
	"context"
	"fmt"
	"slices"
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

// Supplemental to harness-failed-run-inbox.test.ts and generation.ts endFailedRun.
// Canonical withdrawal is part of the same end-run commit. The legacy bounded
// recovery fixture expected a completing task before queue settlement; that is
// not the canonical runtime's recovery boundary.
func TestSourceV160FailedRunWithdrawsQueueAtomicallyAcrossReopen(t *testing.T) {
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
		queued := make([]ID, 0, 20)
		for n := 0; n < cap(queued); n++ {
			sub, err := c.Submit(bg, Input{Content: fmt.Sprint("queued ", n), WhenBusy: "followUp"})
			if err != nil {
				t.Fatal(err)
			}
			statusFailed140(t, sub, "queued", "")
			queued = append(queued, sub.ID())
		}
		write, err := c.Submit(bg, Input{Type: "write", Content: "write"})
		if err != nil {
			t.Fatal(err)
		}
		statusFailed140(t, write, "queued", "")
		last := queued[len(queued)-1]
		commits := make(chan []SubmissionRecord, 1)
		_, observer, err := h.SubscribeCommits(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer observer.Stop()
		if err = observer.Start(func(ctx context.Context, frame PublicationFrame) error {
			var settlements []SubmissionRecord
			for _, change := range frame.Publication.Tables {
				if sub := change.Submission; sub != nil && sub.ID >= first.ID() && sub.ID <= last {
					settlements = append(settlements, publicSubmission(*sub))
				}
			}
			if len(settlements) != 0 {
				select {
				case commits <- settlements:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		if got := waitSubmission(t, first).Record; got.Status != "unanswered" || got.Reason != "model_error" || got.Entry == 0 || got.Answer != 0 {
			t.Fatalf("failed run's placed input: %+v", got)
		}
		select {
		case records := <-commits:
			if len(records) != 1+len(queued) || records[0].ID != first.ID() || records[0].Status != "unanswered" || records[0].Reason != "model_error" {
				t.Fatalf("run failure and withdrawal did not publish together: %+v", records)
			}
			// The pinned inbox walks from its last item to its first.
			for i, record := range records[1:] {
				want := queued[len(queued)-1-i]
				if record.ID != want || record.Status != "unanswered" || record.Reason != "run_failed" || record.Entry != 0 || record.Answer != 0 {
					t.Fatalf("withdrawn input %d: %+v, want unplaced %d/run_failed", i, record, want)
				}
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no withdrawal commit published")
		}
		observer.Stop()
		idleFailed140(t, h)
		statusFailed140(t, write, "queued", "")
		if got := calls.Load(); got != 1 {
			t.Fatalf("provider calls before reopen = %d; want 1", got)
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}

		// A new policy neither replays the failed run nor reclaims withdrawn
		// inputs. Only a new submission can bring the preserved write into a run.
		options.Settings = &HarnessSettings{AfterFailedRun: "continue"}
		h2 := openHarness(t, b.reopen(), options)
		for _, id := range append([]ID{first.ID()}, queued...) {
			sub, err := h2.Submission(bg, id)
			if err != nil || sub == nil {
				t.Fatalf("reopen submission %d: %v", id, err)
			}
			reason := "run_failed"
			if id == first.ID() {
				reason = "model_error"
			}
			statusFailed140(t, sub, "unanswered", reason)
		}
		write2, err := h2.Submission(bg, write.ID())
		if err != nil || write2 == nil {
			t.Fatalf("reopen write: %v", err)
		}
		statusFailed140(t, write2, "queued", "")
		c2, err := h2.Conversation(bg, c.ID())
		if err != nil || c2 == nil {
			t.Fatalf("reopen conversation: %v", err)
		}
		fresh, err := c2.Submit(bg, Input{Content: "post-decision"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, fresh).Record; got.Status != "done" || got.Answer == 0 {
			t.Fatalf("fresh input: %+v", got)
		}
		idleFailed140(t, h2)
		statusFailed140(t, write2, "done", "")
		if got := calls.Load(); got != 2 {
			t.Fatalf("provider calls including fresh input = %d; want 2", got)
		}
	})
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
		// Canonical queued inputs live in the inbox without task records. The
		// active run owns the live host that must remain visible after withdrawal.
		inspection, err := h.InspectTasks(bg)
		if err != nil || len(inspection.Tasks) != 1 || inspection.Tasks[0].Record.Kind != "pi.generation" {
			t.Fatalf("expected only the active generation: %+v, err %v", inspection, err)
		}
		owner := inspection.Tasks[0].Record.ID
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
		child := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
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
		if !slices.Contains(targets, child) {
			t.Fatalf("withdrawal hid owned host %d: idle targets %v", child, targets)
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
