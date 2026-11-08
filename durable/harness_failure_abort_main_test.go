package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func mainAbortFailureLive(t *testing.T, state Snapshot, conversation ID) JSON {
	t.Helper()
	var live JSON
	count := 0
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == conversation {
			live = doc.Value
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one actual live document, got %d", count)
	}
	return live
}

// Require ownership created by the real generation request, never a fabricated
// live.run. Numeric IDs, native run.task, and the request phase named intent
// are the Go representation adapters.
func mainAbortFailureOwner(t *testing.T, h *Harness, c *ConversationHandle, sub *SubmissionHandle) (Task, generationCheckpoint, ID) {
	t.Helper()
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	run := generationObject(mainAbortFailureLive(t, state, c.ID())["run"])
	id := submissionEntryID(run["task"])
	task, exists := state.Tasks[id]
	if !exists || id == 0 || task.Kind != "pi.generation" || task.Conversation != c.ID() || task.Status != "running" {
		t.Fatalf("run does not own an actual running generation: run=%v task=%+v", run, task)
	}
	status, err := sub.Status(bg)
	if err != nil {
		t.Fatal(err)
	}
	first := submissionEntryID(run["first"])
	entry, exists := state.Entries[first]
	if status.Status != "placed" || first == 0 || first != status.Entry || !exists || entry.Conversation != c.ID() || legacyEntryValue(entry)["role"] != "user" {
		t.Fatalf("run.first is not this input's actual persisted user entry: run=%v status=%+v entry=%+v", run, status, entry)
	}
	want, err := dtoObject(JSON{"inputs": []any{sub.ID()}}, h.session.limits)
	if err != nil {
		t.Fatal(err)
	}
	if !equalJSONValue(run["inputs"], want["inputs"]) {
		t.Fatalf("actual run inputs: got %v; want [%d]", run["inputs"], sub.ID())
	}
	var cp generationCheckpoint
	if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
		t.Fatal(err)
	}
	if cp.Submission != sub.ID() || cp.InputEntry != first || cp.Phase != "intent" || cp.Model == nil || cp.Attempt != 1 {
		t.Fatalf("request ownership/checkpoint mismatch: %+v", cp)
	}
	return task, cp, first
}

func mainRequireAbortFailure(t *testing.T, state Snapshot, conversation, task, input, first ID, detail *string) {
	t.Helper()
	failure := JSON{"taskId": task, "inputs": []any{input}, "reason": "aborted", "first": first}
	if detail != nil {
		failure["detail"] = *detail
	}
	want, err := dtoObject(JSON{"runFailure": failure}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	live := mainAbortFailureLive(t, state, conversation)
	if detail == nil {
		if value, exists := generationObject(live["runFailure"])["detail"]; exists {
			t.Errorf("ordinary abort detail must be absent, not inherited from a partial: %v", value)
		}
	}
	if !equalJSONValue(live, want) {
		t.Errorf("abort failure projection: got %v; want %v", live, want)
	}
}

// Source-derived ordinary-abort regression: f9a26365568127461c47a07a37dcb42c140a3ac5
// generation.ts abort/endRun and live.ts endRun. Exercise the real request and
// public Conversation.Abort. The error-bearing partial is a native persisted-
// progress control via production commitPartial (as in generation_abort_test),
// not a claim that the stream adapter preserves provider error text in progress.
func TestMainRunFailureOrdinaryAbortProjection(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("errorPartial=%v", partial), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				entered := make(chan struct{})
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					events := make(chan goai.Event)
					go func() { close(entered); <-ctx.Done(); close(events) }()
					return events
				})
				h := openHarness(t, b.store, options)
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "abort this actual run"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				task, cp, first := mainAbortFailureOwner(t, h, c, sub)
				if partial {
					if err := h.commitPartial(task.ID, MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "committed partial"}}, Api: cp.Model.Api, Provider: cp.Model.Provider, Model: cp.Model.ID, ErrorMessage: "partial diagnostic is not an abort detail", Usage: &goai.Usage{Output: 2, TotalTokens: 2}}); err != nil {
						t.Fatal(err)
					}
				}
				if err := c.Abort(bg); err != nil {
					t.Fatal(err)
				}
				settled := waitSubmission(t, sub)
				idleFailed140(t, h)
				result := waitPublicTask(t, h, task.ID)
				if settled.Record.Status != "unanswered" || settled.Record.Reason != "aborted" || settled.Record.Detail != nil || settled.Task.ID != task.ID || result.State.Outcome == nil || result.State.Outcome.Status != "aborted" {
					t.Errorf("ordinary abort settlement/task mismatch: %+v task=%+v", settled.Record, settled.Task)
				}
				if (settled.Message != nil) != partial {
					t.Fatal("abort lost or synthesized its partial", settled.Message)
				}
				if partial && (settled.Message.StopReason != goai.StopReasonAborted || settled.Message.ErrorMessage != "partial diagnostic is not an abort detail" || len(settled.Message.Content) != 1 || settled.Message.Content[0].Text != "committed partial") {
					t.Errorf("error-bearing partial control was lost: %+v", settled.Message)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				mainRequireAbortFailure(t, state, c.ID(), task.ID, sub.ID(), first, nil)
			})
		})
	}
}

// Derived seeded-cutoff control, NOT the full source crash/cancelDeferred
// scenario at f9a26365 harness-restart-failure.test.ts:671+. A real request owns
// live.run first; after joining its host, seed only its abort mark/restart count
// using restart_limit_140_test.go's interrupted-store control. The real startup
// recovery, scheduler cutoff, report, and harness cleanup must keep run-aborted
// distinct from task-faulted/restart_limit. A cutoff must not convert a partial.
func TestMainRunFailureAbortHandlerCutoffProjectionDerived(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered := make(chan struct{})
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			if requests.Add(1) == 1 {
				close(entered)
			}
			events := make(chan goai.Event)
			go func() { <-ctx.Done(); close(events) }()
			return events
		})
		limit := uint64(2)
		reports := make(chan observedReport140, 8)
		options.Settings = &HarnessSettings{RestartLimit: &limit}
		options.OnReportWithInfo = func(ctx context.Context, err error, info ReportInfo) {
			reports <- observedReport140{ctx: ctx, err: err, info: info}
		}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "cut off this actual run's abort handler"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		task, cp, first := mainAbortFailureOwner(t, h, c, sub)
		if err := h.commitPartial(task.ID, MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "must not salvage at abort cutoff"}}, Api: cp.Model.Api, Provider: cp.Model.Provider, Model: cp.Model.ID, ErrorMessage: "partial diagnostic"}); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, h.session.store)
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		// Prove the seed retains the existing task and run, not a surrogate.
		wantInputs, err := dtoObject(JSON{"inputs": []any{sub.ID()}}, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if run := generationObject(mainAbortFailureLive(t, state, c.ID())["run"]); submissionEntryID(run["task"]) != task.ID || submissionEntryID(run["first"]) != first || !equalJSONValue(run["inputs"], wantInputs["inputs"]) {
			t.Fatal("closing the actual host lost its live ownership", run)
		}
		partial := generationObject(generationObject(mainAbortFailureLive(t, state, c.ID())["generation"])["message"])
		if partial["errorMessage"] != "partial diagnostic" {
			t.Fatal("cutoff fixture did not retain the actual committed partial", partial)
		}
		seed := markTask(state.Tasks[task.ID])
		// Modern partials live only in pi.live. Seed that same real message in
		// its old checkpoint slot to exercise legacy conversion at cutoff.
		var legacy messageReceipt
		if err := fromObject(partial, &legacy, h.session.limits); err != nil {
			t.Fatal(err)
		}
		if err := fromObject(seed.Checkpoint, &cp, h.session.limits); err != nil {
			t.Fatal(err)
		}
		cp.Partial = &legacy
		seed.Checkpoint, err = dtoObject(cp, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		seed.Restarts = limit - 1
		if _, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &seed}}}); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, interruptedStore140(t, store, task.ID), options)
		beforeState, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		before, exists, err := h.Task(bg, task.ID)
		if err != nil || !exists || !before.AbortRequested || before.Restarts != limit {
			t.Fatal("seed did not recover an abort handler at its cutoff", before, err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		report := awaitReport140(t, reports)
		result := waitPublicTask(t, h, task.ID)
		detail := fmt.Sprintf("Abort handler of task %d (pi.generation) was cut off 2 times", task.ID)
		if report.info.Type != "task_faulted" || report.info.Reason != "restart_limit" || report.info.Task == nil || report.info.Task.ID != task.ID || report.info.Task.Kind != "pi.generation" || report.info.Task.ConversationID != c.ID() {
			t.Errorf("cutoff report: %+v", report)
		}
		outcome := result.State.Outcome
		if outcome == nil || outcome.Status != "faulted" || outcome.Error == nil || outcome.Error.Message != detail || outcome.Error.Detail == nil || !outcome.Error.Detail.Present || !equalJSONValue(outcome.Error.Detail.Value, JSON{"reason": "restart_limit"}) || result.Restarts != 0 {
			t.Errorf("cutoff task must remain faulted/restart_limit: %+v", result)
		}
		handle, err := h.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		settled := waitSubmission(t, handle)
		idleFailed140(t, h)
		if settled.Record.Status != "unanswered" || settled.Record.Reason != "aborted" || settled.Record.Detail == nil || !settled.Record.Detail.Present || settled.Record.Detail.Value != detail {
			t.Errorf("cutoff run must settle aborted with cutoff detail, not task faulted: %+v", settled.Record)
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainRequireAbortFailure(t, state, c.ID(), task.ID, sub.ID(), first, &detail)
		for _, entry := range state.Entries {
			if entry.Conversation == c.ID() && legacyEntryValue(entry)["role"] == string(goai.RoleAssistant) {
				t.Errorf("abort cutoff must discard the partial, not convert it: %+v", entry)
			}
		}
		for id, doc := range state.Documents {
			if doc.Kind == "pi.usage" && !equalJSONValue(doc.Value, beforeState.Documents[id].Value) {
				t.Error("abort cutoff salvaged legacy partial usage", doc.Value, beforeState.Documents[id].Value)
			}
		}
		if requests.Load() != 1 {
			t.Errorf("cutoff redispatched the request: %d", requests.Load())
		}
	})
}
