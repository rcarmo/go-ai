package faux

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestFaux110DirectAndRegisteredResponseTiming(t *testing.T) {
	api := goai.Api("faux-duration-110")
	r := Register(&Options{Api: string(api), Provider: "faux-duration-110"})
	defer goai.UnregisterApi(api)
	model := &goai.Model{ID: "timed", Api: api, Provider: "faux-duration-110"}
	for _, direct := range []bool{true, false} {
		var events <-chan goai.Event
		if direct {
			events = r.stream(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, nil)
		} else {
			events = goai.Stream(context.Background(), model, &goai.Context{}, nil)
		}
		found := false
		for event := range events {
			if done, ok := event.(*goai.DoneEvent); ok {
				found = true
				if done.Message.DurationMs == nil || *done.Message.DurationMs < 0 {
					t.Fatal("direct/registry timing absent", direct, done.Message)
				}
			}
		}
		if !found {
			t.Fatal("missing terminal")
		}
	}
	var handle goai.DeferredHandle
	for event := range r.stream(context.Background(), model, &goai.Context{}, &goai.StreamOptions{Deferred: &goai.DeferredOptions{}}) {
		if done, ok := event.(*goai.DoneEvent); ok && done.Message.Deferred != nil {
			handle = *done.Message.Deferred
		}
	}
	if handle.ID == "" {
		t.Fatal("deferred handle absent")
	}
	for event := range r.fetchDeferred(context.Background(), model, handle, nil) {
		if done, ok := event.(*goai.DoneEvent); ok && done.Message.DurationMs != nil {
			t.Fatal("later fetched response timed", done.Message)
		}
	}
}
