package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestReaderUTF16MainCommittedEventsAndSnapshot(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		scalar := toolUTF16Scalar(t, `"\ud800"`)
		key := scalar.WTF8()
		entered := make(chan struct{})
		release := make(chan struct{})
		defer close(release)
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "unicode", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "unicode.reader", Version: 1, Execute: func(ctx context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
			requireToolUTF16(t, args[key], 0xd800)
			close(entered)
			select {
			case <-release:
				return ToolResult{Content: "done"}, nil
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
		}}); err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				event := toolAnswer("unicode-reader", "unicode", nil)
				event.Message.Content[0].SetStreamingArguments(map[string]any{key: scalar}, "")
				events <- event
			} else {
				events <- terminal("answer")
			}
			close(events)
			return events
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		stream, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Stop()
		started := make(chan struct{}, 1)
		if err = stream.Start(func(_ context.Context, events []AgentEvent) error {
			for _, event := range events {
				if event.Type == "tool_execution_start" {
					requireToolUTF16(t, event.Args[key], 0xd800)
					event.Args[key] = "listener mutation"
					select {
					case started <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err = conversation.Submit(bg, Input{Content: "go"}); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		select {
		case <-started:
		case <-stream.Closed():
			end, _ := stream.End()
			t.Fatal("reader failed", end)
		case <-time.After(3 * time.Second):
			t.Fatal("typed tool-start event missing")
		}
		current, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer current.Stop()
		snapshot, err := current.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, message := range snapshot.Messages {
			for _, block := range message.Content {
				if block.Type == "toolCall" {
					found = true
					if !block.HasStreamingArguments() {
						t.Error("snapshot lost ownership")
					}
					requireToolUTF16(t, block.Arguments[key], 0xd800)
					block.Arguments[key] = "snapshot mutation"
				}
			}
		}
		if !found {
			t.Fatal("snapshot omitted committed tool call")
		}
		fresh, err := current.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range fresh.Messages {
			for _, block := range message.Content {
				if block.Type == "toolCall" {
					requireToolUTF16(t, block.Arguments[key], 0xd800)
				}
			}
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if err = fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				requireToolUTF16(t, cp.Arguments[key], 0xd800)
			}
		}
	})
}
