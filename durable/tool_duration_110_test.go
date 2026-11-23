package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// v1.6.0 tool.ts:1178 measures from the durable task's start to settlement.
// A call blocked before the executor still ran a tool task and its hooks.
func TestToolDuration110ExecutedErrorPanicAndUnexecuted(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				registry := NewRegistry()
				var executions, clock atomic.Int64
				clock.Store(1_000)
				{
					reg := wrapRegistration("timed")
					reg.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
						executions.Add(1)
						clock.Add(30)
						if mode == "panic" {
							panic("timed panic")
						}
						if mode == "error" {
							return ToolResult{}, errors.New("timed error")
						}
						return ToolResult{Content: "timed result"}, nil
					}
					if e := registry.Register(reg); e != nil {
						t.Fatal(e)
					}
				}
				if e := registry.Install(&Extension{Name: "timing-hook", ToolHooks: ToolHooks{BeforeTool: func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error) {
					clock.Add(100)
					if mode == "blocked" {
						return &BeforeToolResult{Block: "blocked"}, nil
					}
					return nil, nil
				}, AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
					clock.Add(75)
					return &result, nil
				}}}); e != nil {
					t.Fatal(e)
				}
				var calls atomic.Int64
				var observedDuration *int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if calls.Add(1) == 1 {
						ch <- toolAnswer("timed-call", "timed", JSON{})
					} else {
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult && message.DurationMs != nil {
								value := *message.DurationMs
								observedDuration = &value
							}
						}
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				options.Now = clock.Load
				h := openHarness(t, b.store, options)
				conversation := root(t, h, ref)
				sub, e := conversation.Submit(bg, Input{Content: "time tool"})
				if e != nil {
					t.Fatal(e)
				}
				if result := waitSubmission(t, sub); result.Submission.Status != "done" {
					t.Fatal(result)
				}
				tasks := nestedKeyToolTasks(t, conversation)
				receipt := v160ToolResult(t, bg, conversation, "timed-call")
				wantElapsed, wantExecutions := int64(205), int64(1)
				if mode == "blocked" {
					wantElapsed, wantExecutions = 175, 0
				}
				if len(tasks) != 1 || tasks[0].StartedAt == nil || *tasks[0].StartedAt != 1_000 || receipt.Timestamp != 1_000+wantElapsed {
					t.Fatal("durable task timing", tasks, receipt.Timestamp)
				}
				if receipt.DurationMs == nil || *receipt.DurationMs != wantElapsed || executions.Load() != wantExecutions {
					t.Fatalf("tool receipt duration %v, executions %d; want %d ms and %d executions", receipt.DurationMs, executions.Load(), wantElapsed, wantExecutions)
				}
				if observedDuration == nil || *observedDuration != wantElapsed {
					t.Fatal("model context lost task-elapsed duration", observedDuration)
				}
				message := receiptMessage(receipt)
				if message.DurationMs == nil || *message.DurationMs != wantElapsed {
					t.Fatal("projection lost duration", message)
				}
				*message.DurationMs = 999
				if *receipt.DurationMs != wantElapsed {
					t.Fatal("duration pointer alias")
				}
				if receipt.IsError != (mode != "success") {
					t.Fatal("unexpected error outcome", receipt)
				}
			})
		})
	}
}
