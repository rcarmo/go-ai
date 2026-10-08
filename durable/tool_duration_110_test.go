package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
	"time"
)

func TestToolDuration110ExecutedErrorPanicAndUnexecuted(t *testing.T) {
	for _, mode := range []string{"success", "error", "panic", "blocked"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var executions atomic.Int64
				{
					reg := wrapRegistration("timed")
					reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
						executions.Add(1)
						time.Sleep(8 * time.Millisecond)
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
				// Hooks execute outside the measured executor attempt.
				if e := registry.Install(&Extension{Name: "timing-hook", ToolHooks: ToolHooks{BeforeTool: func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error) {
					if mode == "blocked" {
						return &BeforeToolResult{Block: "blocked"}, nil
					}
					return nil, nil
				}, AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
					time.Sleep(75 * time.Millisecond)
					return &result, nil
				}}}); e != nil {
					t.Fatal(e)
				}
				var calls atomic.Int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if calls.Add(1) == 1 {
						ch <- toolAnswer("timed-call", "timed", JSON{})
					} else {
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				conversation := root(t, h, ref)
				sub, e := conversation.Submit(bg, Input{Content: "time tool"})
				if e != nil {
					t.Fatal(e)
				}
				if result := waitSubmission(t, sub); result.Submission.Status != "done" {
					t.Fatal(result)
				}
				state, e := h.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				found := false
				for _, task := range state.Tasks {
					if task.Kind != "pi.tool" {
						continue
					}
					var cp toolCheckpoint
					if e := fromObject(task.Checkpoint, &cp, h.session.limits); e != nil {
						t.Fatal(e)
					}
					if cp.Result == nil {
						t.Fatal("missing result")
					}
					found = true
					if mode == "blocked" {
						if cp.Result.DurationMs != nil || executions.Load() != 0 {
							t.Fatal("unexecuted call timed", cp.Result)
						}
					} else {
						if cp.Result.DurationMs == nil || *cp.Result.DurationMs < 1 || executions.Load() != 1 {
							t.Fatal("executor timing absent", cp.Result, executions.Load())
						}
						// Compare checkpoint and protocol fields without a scheduler-dependent
						// absolute upper bound.
						if cp.DurationMs == nil || *cp.Result.DurationMs != *cp.DurationMs {
							t.Fatal("timing changed during hooks/settlement", cp)
						}
						message := receiptMessage(*cp.Result)
						if message.DurationMs == nil || *message.DurationMs != *cp.Result.DurationMs {
							t.Fatal("projection lost duration", message)
						}
						*message.DurationMs = 999
						if *cp.Result.DurationMs == 999 {
							t.Fatal("duration pointer alias")
						}
					}
				}
				if !found {
					t.Fatal("missing tool task")
				}
			})
		})
	}
}
