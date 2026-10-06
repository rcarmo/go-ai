package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerationDurableRetryUsageAndReopenDeadline(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var requests atomic.Int64
		var clock atomic.Int64
		clock.Store(1000)
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonError, ErrorMessage: "overloaded_error", Usage: &goai.Usage{Input: 1, TotalTokens: 1}}}
			} else {
				ch <- terminal("retried")
			}
			close(ch)
			return ch
		})
		options.Now = clock.Load
		first := openHarness(t, b.store, options)
		conversation, err := first.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Retry: RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMs: 1000, MaxDelayMs: 60000}}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "retry"})
		if err != nil {
			t.Fatal(err)
		}
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		var taskID ID
		for {
			state, err := first.Snapshot(deadline)
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range state.Tasks {
				if task.Kind == "pi.generation" {
					var cp generationCheckpoint
					if err := fromObject(task.Checkpoint, &cp, first.session.limits); err != nil {
						t.Fatal(err)
					}
					if cp.Phase == "retry" {
						taskID = task.ID
						if cp.RetryUntil != 2000 || cp.RetryCount != 1 {
							t.Fatal(cp)
						}
					}
				}
			}
			if taskID != 0 {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("retry checkpoint missing")
			default:
			}
		}
		before, err := first.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		errorEntries := 0
		for _, entry := range before.Entries {
			if entry.Value["role"] == string(goai.RoleAssistant) && entry.Value["stopReason"] == string(goai.StopReasonError) {
				errorEntries++
				if entry.Value["errorMessage"] != "overloaded_error" {
					t.Fatal("retry error text lost", entry)
				}
			}
		}
		if errorEntries != 1 {
			t.Fatal("retry failed attempt not appended", errorEntries)
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		if requests.Load() != 1 {
			t.Fatal("retry ignored clock", requests.Load())
		}
		clock.Store(2000)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		pending, ok, err := second.Task(bg, taskID)
		if err != nil || !ok || pending.State.Outcome != nil {
			t.Fatal("retry recovered terminal", pending, err)
		}
		handle, err := second.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || result.Message.Content[0].Text != "retried" || requests.Load() != 2 {
			t.Fatal(result, requests.Load())
		}
		state, err := second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == "pi.usage" {
				models := doc.Value["models"].(map[string]any)
				var usage goai.Usage
				if err := fromObject(JSON(models[string(ref.Provider)+"/"+ref.ID].(map[string]any)), &usage, second.session.limits); err != nil || usage.TotalTokens != 6 {
					t.Fatal("retry usage", usage, err)
				}
			}
		}
	})
}
