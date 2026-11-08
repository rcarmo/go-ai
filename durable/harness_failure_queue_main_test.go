package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived regression, f9a26365 generation.ts:642-654,887-888,1162-1172:
// endFailedRun starts only its immediate successor with keepFailure. After that
// successor answers, normal startRun clears the failure even for input that was
// already queued when the original run failed. Each run has its own first entry.
func TestMainRunFailureContinueQueueClearsFailureAfterAnswer(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var calls atomic.Int64
		entered := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
		release := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			n := calls.Add(1)
			go func() {
				defer close(ch)
				if n > int64(len(entered)) {
					t.Errorf("unexpected provider call %d", n)
					ch <- terminal("unexpected answer")
					return
				}
				close(entered[n-1])
				select {
				case <-release[n-1]:
				case <-ctx.Done():
					return
				}
				if n == 1 {
					msg := terminal("A failed").Message
					msg.StopReason, msg.ErrorMessage = goai.StopReasonError, "A failure"
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: msg}
				} else if n == 2 {
					ch <- terminal("B answer")
				} else {
					ch <- terminal("C answer")
				}
			}()
			return ch
		})
		options.Settings = &HarnessSettings{AfterFailedRun: "continue", FollowUpMode: "one-at-a-time"}
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release...)
		c := root(t, h, ref)

		// Provider gates make these committed in-progress observations, not
		// timing-dependent snapshots taken after the run has already settled.
		active := func(sub *SubmissionHandle, name string) (JSON, ID) {
			t.Helper()
			status, err := sub.Status(bg)
			if err != nil || status.Status != "placed" || status.Entry == 0 {
				t.Fatalf("%s must be placed while its provider is gated: %+v, %v", name, status, err)
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			var live JSON
			for _, doc := range state.Documents {
				if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == c.ID() {
					if live != nil {
						t.Fatal("multiple live documents")
					}
					live = doc.Value
				}
			}
			if live == nil {
				t.Fatal("missing live document")
			}
			run := generationObject(live["run"])
			task, ok := state.Tasks[submissionEntryID(run["task"])]
			if !ok || task.Kind != "pi.generation" || task.Conversation != c.ID() || terminalStatus(task.Status) {
				t.Fatalf("%s has no active generation: %v", name, live)
			}
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
				t.Fatal(err)
			}
			if cp.Submission != sub.ID() || cp.InputEntry != status.Entry || submissionEntryID(run["first"]) != status.Entry {
				t.Fatalf("%s must start at its own checkpoint input entry: run=%v checkpoint=%+v status=%+v", name, run, cp, status)
			}
			want, err := dtoObject(JSON{"inputs": []any{sub.ID()}}, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			if !equalJSONValue(run["inputs"], want["inputs"]) {
				t.Fatalf("%s must own only its input: %v", name, run)
			}
			entry, ok := state.Entries[status.Entry]
			if !ok || entry.Conversation != c.ID() || legacyEntryValue(entry)["role"] != "user" {
				t.Fatalf("%s first must be its persisted user entry: %+v", name, entry)
			}
			if held, _ := live["failedRun"].(bool); held {
				t.Fatalf("%s continuation must not hold the queue: %v", name, live)
			}
			return live, status.Entry
		}
		a, err := c.Submit(bg, Input{Content: "A"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered[0])
		_, aEntry := active(a, "A")
		bSub, err := c.Submit(bg, Input{Content: "B"})
		if err != nil {
			t.Fatal(err)
		}
		cSub, err := c.Submit(bg, Input{Content: "C"})
		if err != nil {
			t.Fatal(err)
		}
		for _, sub := range []*SubmissionHandle{bSub, cSub} {
			status, err := sub.Status(bg)
			if err != nil || status.Status != "queued" || status.Entry != 0 {
				t.Fatalf("B and C must both be unplaced before A fails: %+v, %v", status, err)
			}
		}
		close(release[0])
		failed := waitSubmission(t, a)
		if failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" || failed.Record.Entry != aEntry {
			t.Fatalf("A must fail at its own input entry: %+v", failed.Record)
		}
		awaitTaskSignal(t, entered[1])
		live, bEntry := active(bSub, "B")
		if bEntry == aEntry {
			t.Fatal("B reused the failed A entry as its first")
		}
		wantFailure, err := dtoObject(JSON{"taskId": failed.Task.ID, "inputs": []any{a.ID()}, "reason": "model_error", "detail": "A failure", "first": aEntry}, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if !equalJSONValue(live["runFailure"], wantFailure) {
			t.Errorf("B in progress must retain A failure: got %v; want %v", live["runFailure"], wantFailure)
		}
		statusFailed140(t, cSub, "queued", "")
		close(release[1])
		answered := waitSubmission(t, bSub)
		if answered.Record.Status != "done" || answered.Record.Answer == 0 {
			t.Fatalf("B must answer before C starts: %+v", answered.Record)
		}
		awaitTaskSignal(t, entered[2])
		live, cEntry := active(cSub, "C")
		if cEntry == aEntry || cEntry == bEntry {
			t.Fatal("C must start at its own entry, not A or B", aEntry, bEntry, cEntry)
		}
		if failure, exists := live["runFailure"]; exists {
			t.Errorf("C in progress must clear A failure after B answered; keepFailure applies only to the immediate successor: %v", failure)
		}
		close(release[2])
		if result := waitSubmission(t, cSub); result.Record.Status != "done" {
			t.Fatalf("C must answer: %+v", result.Record)
		}
		idleFailed140(t, h)
		if calls.Load() != 3 {
			t.Fatal("expected exactly A, B and C provider calls", calls.Load())
		}
	})
}
