package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestInvalidToolResultWithAfterToolStillFaultsWithoutReceipt(t *testing.T) {
	for _, where := range []string{"executor", "hook"} {
		t.Run(where, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				registration := wrapRegistration("invalid-return")
				cycle := JSON{}
				cycle["self"] = cycle
				registration.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					if where == "executor" {
						return ToolResult{Details: cycle}, nil
					}
					return ToolResult{Content: "valid"}, nil
				}
				if err := registry.Register(registration); err != nil {
					t.Fatal(err)
				}
				hookCalls := 0
				if err := registry.Install(&Extension{Name: "invalid-result-hook", ToolHooks: ToolHooks{AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
					hookCalls++
					if where == "hook" {
						result.Details = cycle
					}
					return &result, nil
				}}}); err != nil {
					t.Fatal(err)
				}
				calls := 0
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					calls++
					ch := make(chan goai.Event, 1)
					if calls == 1 {
						ch <- toolAnswer("invalid-call", "invalid-return", JSON{})
					} else {
						ch <- terminal("continued")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				sub, err := root(t, h, ref).Submit(bg, Input{Content: "invalid"})
				if err != nil {
					t.Fatal(err)
				}
				if result := waitSubmission(t, sub); result.Submission.Status != "done" {
					t.Fatal(result)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range state.Tasks {
					if task.Kind == "pi.tool" {
						record, err := CanonicalTask(task, h.session.limits)
						if err != nil || record.State.Outcome.Status != "faulted" {
							t.Fatal(record, err)
						}
					}
				}
				for _, entry := range state.Entries {
					if entry.Value["role"] == string(goai.RoleToolResult) {
						t.Fatal("invalid result invented receipt", entry)
					}
				}
				want := 1
				if where == "executor" {
					want = 0
				}
				if hookCalls != want {
					t.Fatal("hook call count", hookCalls, want)
				}
			})
		})
	}
}
