package durable

import (
	"context"
	"errors"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestThrownToolErrorRetainsPartialAndOriginalDiagnosticAndFailedOutcome(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		reg := wrapRegistration("throws")
		reg.Execute = func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			if err := api.Output("partial"); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{}, errors.New("boom")
		}
		if err := registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		afterCalls := 0
		extension := &Extension{Name: "throw-observer", ToolHooks: ToolHooks{AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			afterCalls++
			if !result.IsError || result.Content != "partial" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "boom" {
				t.Errorf("afterTool did not see thrown result %+v", result)
			}
			result.Content = "hook replacement"
			result.IsError = false
			result.Diagnostics = []ToolDiagnostic{{Severity: "error", Code: "hook", Message: "rewritten diagnostic"}}
			return &result, nil
		}}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("throws-call", "throws", JSON{})
			} else {
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						text := ""
						for _, block := range message.Content {
							text += block.Text
						}
						if message.IsError || !strings.Contains(text, "hook replacement") || !strings.Contains(text, "[error] rewritten diagnostic") {
							t.Errorf("throw result: %+v", message)
						}
					}
				}
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		sub, err := root(t, h, ref).Submit(bg, Input{Content: "throw"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if afterCalls != 1 {
			t.Fatal("afterTool throw count", afterCalls)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				record, err := CanonicalTask(task, h.session.limits)
				if err != nil || record.State.Outcome.Status != "failed" || record.State.Outcome.Error.Message != "Tool throws threw" {
					t.Fatal("thrown error changed", record, err)
				}
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil || len(cp.Diagnostics) != 1 || cp.Diagnostics[0].Message != "rewritten diagnostic" || cp.Result.Diagnostics[0].Message != "rewritten diagnostic" {
					t.Fatal("throw diagnostic changed", cp, err)
				}
			}
		}
	})
}
