package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestToolControlsUnavailableOrThrowingCallCannotTerminateRound(t *testing.T) {
	for _, mode := range []string{"unavailable", "throwing", "invalid-control"} {
		t.Run(mode, func(t *testing.T) {
			registry := NewRegistry()
			var entryHooks atomic.Int64
			if err := registry.Install(&Extension{Name: "result-entry-contract", Hooks: GenerationHooks{AfterToolEntries: func(ctx context.Context, assistant ID, results []ID, api *HookAPI) error {
				entryHooks.Add(1)
				want := 2
				if mode == "invalid-control" {
					want = 1 // invalid result has no committed receipt
				}
				if len(results) != want {
					t.Errorf("result entry count: got %d want %d", len(results), want)
					return nil
				}
				for i, id := range results {
					entry, ok, err := api.Entry(ctx, id)
					wantCall := []string{"first-call", "second-call"}[i]
					if err != nil || !ok || entry.ID <= assistant || entry.Value["toolCallId"] != wantCall {
						t.Errorf("ordered committed result %d: %+v %v", i, entry, err)
					}
				}
				return nil
			}}}); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"first", "second"} {
				name := name
				if mode == "unavailable" && name == "second" {
					continue
				}
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "negative." + name, Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					if name == "second" && mode == "throwing" {
						return ToolResult{Control: &ToolControl{Terminate: true}}, errors.New("private")
					}
					if name == "second" && mode == "invalid-control" {
						return ToolResult{Control: &ToolControl{Terminate: true, AddTools: []string{"invalid/name"}}}, nil
					}
					return ToolResult{Content: "ok", Control: &ToolControl{Terminate: true}}, nil
				}}); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if calls.Add(1) == 1 {
					answer := terminal("calls")
					answer.Message.StopReason = goai.StopReasonToolUse
					answer.Reason = goai.StopReasonToolUse
					answer.Message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "first-call", Name: "first", Arguments: map[string]any{}}, {Type: "toolCall", ID: "second-call", Name: "second", Arguments: map[string]any{}}}
					ch <- answer
				} else {
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			store, _ := NewMemory()
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			sub, err := c.Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, sub)
			if mode == "unavailable" {
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				tools, unavailable := 0, 0
				for _, task := range state.Tasks {
					if task.Kind == "pi.tool" {
						tools++
					}
				}
				for _, entry := range state.Entries {
					if entry.Kind != "message" {
						continue
					}
					var receipt MessageReceipt
					if err := fromObject(entry.Value, &receipt, h.session.limits); err == nil && receipt.Role == goai.RoleToolResult && receipt.ToolCallID == "second-call" {
						if !receipt.IsError || receipt.ErrorCode != "tool_unavailable" {
							t.Fatal("unoffered result", receipt)
						}
						unavailable++
					}
				}
				if tools != 1 || unavailable != 1 {
					t.Fatal("unoffered call created task or missing immediate result", tools, unavailable)
				}
			}
			if calls.Load() != 2 || entryHooks.Load() != 1 {
				t.Fatal("failed/no-task call terminated or hook skipped", calls.Load(), entryHooks.Load())
			}
		})
	}
}
func TestToolTerminateFinalBoundaryQueuesFollowUpAndPreservesHookControl(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		entered, release := make(chan struct{}), make(chan struct{})
		ext := &Extension{Name: "hook-control", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "end", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "end.test", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			close(entered)
			<-release
			return ToolResult{Content: "ended"}, nil
		}}}, ToolHooks: ToolHooks{AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			result.Control = &ToolControl{Terminate: true}
			return &result, nil
		}}}
		if err := registry.Install(ext); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			n := calls.Add(1)
			ch := make(chan goai.Event, 1)
			if n == 1 {
				ch <- toolAnswer("end-call", "end", JSON{})
			} else {
				if input.Messages[len(input.Messages)-1].Content[0].Text != "follow-up" {
					t.Error("followup final boundary", input.Messages)
				}
				ch <- terminal("follow-up answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		next, err := c.Submit(bg, Input{Content: "follow-up", Type: "follow-up"})
		if err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		firstResult := waitSubmission(t, first)
		secondResult := waitSubmission(t, next)
		if firstResult.Message.StopReason != goai.StopReasonToolUse || secondResult.Message.Content[0].Text != "follow-up answer" || calls.Load() != 2 {
			t.Fatal("terminate boundary outputs", firstResult, secondResult, calls.Load())
		}
	})
}
