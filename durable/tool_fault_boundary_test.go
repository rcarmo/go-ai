package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

// Strict native receipts reject invalid JSON before storage. This fixture uses
// the scheduler's real fault/orphan deciding path on a recovered owned tool to
// prove the same missing-result continuation without fabricating a receipt.
func TestToolSchedulerFaultOrphanMissingResultContinuationAndReopen(t *testing.T) {
	for _, status := range []string{"faulted", "orphaned"} {
		t.Run(status, func(t *testing.T) {
			var effects, calls atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			registry := NewRegistry()
			reg := wrapRegistration("work")
			reg.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				close(entered)
				<-release
				<-ctx.Done()
				return ToolResult{}, ctx.Err()
			}
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if calls.Add(1) == 1 {
					ch <- toolAnswer("work-call", "work", JSON{})
				} else {
					count := 0
					for _, message := range input.Messages {
						if message.Role == goai.RoleToolResult {
							count++
							if !message.IsError || message.ToolCallID != "work-call" || len(message.Content) != 1 || !strings.Contains(message.Content[0].Text, "Tool result unavailable: history ends before this call completed.") || !equalJSONValue(message.Details, JSON{"reason": "missing_result"}) {
								t.Error("missing-result model context", message)
							}
						}
					}
					if count != 1 {
						t.Error("missing result count", count)
					}
					ch <- terminal("continued")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			store, dir := newJournal(t)
			h := openHarness(t, store, options)
			sub, err := root(t, h, ref).Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			closed := make(chan error, 1)
			go func() { closed <- h.Close(bg) }()
			<-h.life.Done()
			close(release)
			if err := <-closed; err != nil {
				t.Fatal(err)
			}
			store, err = OpenJournal(dir, JournalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, store, options)
			snapshot, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			var child ID
			for _, task := range snapshot.Tasks {
				if task.Kind == "pi.tool" {
					child = task.ID
				}
			}
			if child == 0 || effects.Load() != 1 {
				t.Fatal("recovered child", child, effects.Load())
			}
			outcome := TaskOutcome{Status: status}
			if status == "faulted" {
				outcome.Error = &TaskOutcomeError{Message: "scheduler_fixture_fault"}
			} else {
				outcome.Reason = "missing_builtin_phase"
			}
			_, err = h.session.taskCommit(bg, func(tx *Tx) error { return h.scheduler.builtinDecision(tx, tx.state.Tasks[child], outcome) })
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			store, err = OpenJournal(dir, JournalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, store, options)
			if effects.Load() != 1 || calls.Load() != 1 {
				t.Fatal("open dispatched", effects.Load(), calls.Load())
			}
			resumed, err := h.Submission(bg, sub.ID())
			if err != nil {
				t.Fatal(err)
			}
			result := waitSubmission(t, resumed)
			if result.Submission.Status != "done" || effects.Load() != 1 || calls.Load() != 2 {
				t.Fatal("fault continuation", result, effects.Load(), calls.Load())
			}
			record, err := h.WaitForTask(bg, child)
			if err != nil || record.State.Outcome == nil || record.State.Outcome.Status != status {
				t.Fatal("terminal decision", record, err)
			}
			snapshot, err = h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range snapshot.Entries {
				if entry.ByTask == child {
					t.Fatal("fabricated persistent tool result", entry)
				}
			}
			var cp toolCheckpoint
			if err := fromObject(snapshot.Tasks[child].Checkpoint, &cp, h.session.limits); err != nil {
				t.Fatal(err)
			}
			if cp.Result != nil {
				t.Fatal("fabricated result checkpoint", cp.Result)
			}
		})
	}
}
