package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestGenerationModelFailurePreservesProviderTextReasonAndReceipt(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "billing limit reached", Content: []goai.ContentBlock{}}}
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		sub, err := root(t, h, ref).Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		if result.Submission.Status != "failed" || result.Message == nil || result.Message.ErrorMessage != "billing limit reached" || result.Submission.Value["errorCode"] != "model_error" {
			t.Fatal(result)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			record, err := CanonicalTask(task, h.session.limits)
			if err != nil || record.State.Outcome.Error.Message != "billing limit reached" || record.State.Outcome.Error.Detail.Value.(map[string]any)["reason"] != "model_error" {
				t.Fatal(record, err)
			}
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := h.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		if reopened := waitSubmission(t, handle); reopened.Message.ErrorMessage != "billing limit reached" {
			t.Fatal(reopened)
		}
	})
}
