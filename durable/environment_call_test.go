package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestToolEnvironmentConstructedOnceBeforeExecuteAndNoUnavailableCall(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var factories, effects atomic.Int64
		registry := NewRegistry()
		registration := wrapRegistration("environment-call")
		registration.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			if factories.Load() != 2 { // Prompt preparation then execution environment.
				t.Error("environment was not prepared before Execute", factories.Load())
			}
			first, err := api.Environment(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			second, err := api.Environment(ctx)
			if err != nil {
				return ToolResult{}, err
			}
			if first != second || first.Cwd() != "/call" {
				t.Error("environment changed within call", first, second)
			}
			effects.Add(1)
			return ToolResult{Content: "executed"}, nil
		}
		if err := registry.Register(registration); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				event := toolAnswer("env-call", "environment-call", JSON{})
				event.Message.Content = append(event.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "absent", Name: "unavailable", Arguments: map[string]any{}})
				ch <- event
			} else {
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		options.Env = func(context.Context, EnvTarget) (ExecutionEnvironment, error) {
			factories.Add(1)
			return testEnvironment{"/call"}, nil
		}
		h := openHarness(t, b.store, options)
		sub, err := root(t, h, ref).Submit(bg, Input{Content: "env"})
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, sub); result.Submission.Status != "done" || factories.Load() != 3 || effects.Load() != 1 {
			t.Fatal(result, factories.Load(), effects.Load())
		}
	})
}
