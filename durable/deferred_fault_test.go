package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestDeferredPollThrowFaultsTaskWithoutAssistantErrorReceipt(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		api := goai.Api("deferred-fault-" + t.Name())
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonDeferred, Content: []goai.ContentBlock{}, Deferred: &goai.DeferredHandle{ID: "job", PollAfterMs: 1}}}
			close(ch)
			return ch
		}, FetchDeferred: func(context.Context, *goai.Model, goai.DeferredHandle, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- &goai.ErrorEvent{Err: errors.New("poll callback failed")}
			close(ch)
			return ch
		}})
		defer goai.UnregisterApi(api)
		h := openHarness(t, b.store, Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }})
		sub, err := root(t, h, ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}).Submit(bg, Input{Content: "poll"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		if result.Submission.Status != "failed" || result.Message != nil {
			t.Fatal(result)
		}
		record, err := CanonicalTask(result.Task, h.session.limits)
		if err != nil || record.State.Outcome.Status != "faulted" || record.State.Outcome.Error.Message != "poll callback failed" {
			t.Fatal(record, err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range state.Entries {
			if entry.Value["role"] == string(goai.RoleAssistant) {
				t.Fatal("poll throw invented receipt", entry)
			}
		}
	})
}
