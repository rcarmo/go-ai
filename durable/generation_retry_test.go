package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestGenerationDurableRetryUsageAndReopenDeadline(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
		wakes := make(chan Wake, 128)
		options.OnWake = func(w Wake) { wakes <- copyWake(w) }
		enabled, maximum, delay := true, 1, int64(1000)
		options.Settings = &HarnessSettings{Retry: &RetrySettings{Enabled: &enabled, MaxRetries: &maximum, BaseDelayMs: &delay}}
		first := openHarness(t, b.store, options)
		conversation, err := first.Root(bg, AgentChange{Model: ref})
		if err != nil {
			t.Fatal(err)
		}
		publications := recordGenerationPublications(t, first)
		sub, err := conversation.Submit(bg, Input{Content: "retry"})
		if err != nil {
			t.Fatal(err)
		}
		retryTask := publications.waitTask(t, "retry", "sleeping")
		taskID := retryTask.ID
		cp := taskCheckpointMap(retryTask.State.Checkpoint)
		if submissionEntryID(cp["until"]) != 2000 || submissionEntryID(cp["attempt"]) != 1 || retryTask.State.Until == nil || *retryTask.State.Until != 2000 {
			t.Fatal("stored retry", retryTask)
		}
		until := int64(2000)
		awaitWake140(t, wakes, false, &until)
		before, err := first.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		errorEntries := 0
		for _, entry := range before.Entries {
			if legacyEntryValue(entry)["role"] == string(goai.RoleAssistant) && legacyEntryValue(entry)["stopReason"] == string(goai.StopReasonError) {
				errorEntries++
				if legacyEntryValue(entry)["errorMessage"] != "overloaded_error" {
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
