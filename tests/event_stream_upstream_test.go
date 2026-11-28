package goai_test

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV170EventStreamStopsAtFirstTerminal(t *testing.T) {
	const api goai.Api = "test-v170-event-stream-terminal"
	goai.RegisterApi(&goai.ApiProvider{
		Api: api,
		Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 4)
			ch <- &goai.TextDeltaEvent{Delta: "before", Partial: &goai.Message{}}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonStop, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "first"}}}}
			ch <- &goai.TextDeltaEvent{Delta: "after", Partial: &goai.Message{}}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonStop, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "second"}}}}
			close(ch)
			return ch
		},
	})
	model := &goai.Model{Api: api, ID: "terminal", Provider: "test"}

	var events []goai.Event
	for event := range goai.Stream(context.Background(), model, &goai.Context{}, nil) {
		events = append(events, event)
	}
	if len(events) != 2 {
		t.Fatalf("stream exposed %d events after its terminal event; want 2", len(events))
	}
	terminal, ok := events[1].(*goai.DoneEvent)
	if !ok || terminal.Message == nil || terminal.Message.Content[0].Text != "first" {
		t.Fatalf("terminal=%#v, want first done event", events[1])
	}

	message, err := goai.Complete(context.Background(), model, &goai.Context{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if message == nil || len(message.Content) != 1 || message.Content[0].Text != "first" {
		t.Fatalf("Complete result=%#v, want first terminal message", message)
	}
}
