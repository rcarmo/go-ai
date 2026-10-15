package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestTool130MalformedArgumentsNeverExecute(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects, requests atomic.Int64
		reg := wrapRegistration("bad_args")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "unexpected"}, nil
		}
		if err := registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		api := goai.Api("bad-args-130")
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
			if requests.Add(1) == 1 {
				message.StopReason = goai.StopReasonToolUse
				message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "bad", Name: "bad_args", Arguments: map[string]interface{}{}, ArgumentsError: "invalid JSON object\n[1]"}}
			} else {
				last := input.Messages[len(input.Messages)-1]
				if last.Role != goai.RoleToolResult || !last.IsError {
					t.Error("missing error result", last)
				}
			}
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
			close(ch)
			return ch
		}})
		defer goai.UnregisterApi(api)
		h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
			return &goai.Model{Provider: "test", ID: "model", Api: api, Input: []string{"text"}, MaxTokens: 100, ContextWindow: 10000}
		}})
		conv, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "test", ID: "model"}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conv.Submit(bg, Input{Content: "run"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		result, err := sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || effects.Load() != 0 || requests.Load() != 2 {
			t.Fatal(result, err, effects.Load(), requests.Load())
		}
	})
}
