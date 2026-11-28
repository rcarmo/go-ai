package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
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
					if err != nil || !ok || entry.ID <= assistant || entry.Kind != "pi.tool-result" || len(entry.Model) != 1 || entry.Model[0].Role != goai.RoleToolResult || entry.Model[0].ToolCallID != wantCall {
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
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "negative." + name, Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
					if name == "second" && mode == "throwing" {
						return ToolResult{Control: &ToolControl{Terminate: true}}, errors.New("private")
					}
					if name == "second" && mode == "invalid-control" {
						return ToolResult{Control: &ToolControl{Terminate: true, AddTools: make([]string, DefaultLimits().MaxPage+1)}}, nil
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
			store, _ := newTestSQLite(t)
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
				tools, unavailable, flagged := 0, 0, 0
				for _, task := range state.Tasks {
					if task.Kind == "pi.tool" {
						tools++
						record, found, err := h.Task(bg, task.ID)
						if err != nil || !found || record.Input == nil {
							t.Fatal(record, err)
						}
						input := generationObject(record.Input.Value)
						if input["callId"] == "second-call" {
							if input["unoffered"] != true || input["kind"] != "model" || record.Version != 4 {
								t.Fatal("unoffered canonical tool input", record)
							}
							flagged++
						}
					}
				}
				for _, entry := range state.Entries {
					if !isMessageEntry(entry) {
						continue
					}
					var receipt MessageReceipt
					if err := decodeEntryMessage(entry, h.session.limits, &receipt); err == nil && receipt.Role == goai.RoleToolResult && receipt.ToolCallID == "second-call" {
						if !receipt.IsError || receipt.ErrorCode != "" || len(receipt.Diagnostics) != 1 || receipt.Diagnostics[0].Severity != "error" || receipt.Diagnostics[0].Code != "tool_unavailable" || receipt.Diagnostics[0].Message != "Tool second is not available" || len(receipt.Content) != 1 || receipt.Content[0].Text != "<harness>\n[error] Tool second is not available\n</harness>" {
							t.Fatal("unoffered result", receipt)
						}
						unavailable++
					}
				}
				if tools != 2 || unavailable != 1 || flagged != 1 {
					t.Fatal("unoffered call must own a flagged tool task and one result", tools, unavailable, flagged)
				}
			}
			if calls.Load() != 2 || entryHooks.Load() != 1 {
				t.Fatal("failed/no-task call terminated or hook skipped", calls.Load(), entryHooks.Load())
			}
		})
	}
}

// Source: harness-tool-hooks.test.ts, unoffered calls still own a tool task
// and run afterTool on that task. Hook control then owns the round boundary.
func TestToolHookOwnsUnavailableCallAndControlsRound(t *testing.T) {
	for _, terminate := range []bool{false, true} {
		name := fmt.Sprintf("terminate=%v", terminate)
		t.Run(name, func(t *testing.T) {
			registry := NewRegistry()
			var requests atomic.Int64
			var hookTask ID
			if err := registry.Install(&Extension{Name: "unoffered-hook", ToolHooks: ToolHooks{
				AfterTool: func(_ context.Context, call goai.ToolCall, result ToolResult, api *HookAPI) (*ToolResult, error) {
					if call.ID != "ghost-call" || !result.IsError || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "tool_unavailable" {
						t.Error("afterTool did not receive the unavailable call", call, result)
					}
					hookTask = api.TaskID()
					result.Content = "try another tool"
					if terminate {
						result.Control = &ToolControl{Terminate: true}
					}
					return &result, nil
				},
			}}); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				out := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					out <- toolAnswer("ghost-call", "ghost", JSON{})
				} else {
					out <- terminal("done")
				}
				close(out)
				return out
			})
			options.Registry = registry
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			submission, err := conversation.Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			if settled := waitSubmission(t, submission); settled.Record.Status != "done" {
				t.Fatal("submission", settled)
			}
			wantRequests := int64(2)
			if terminate {
				wantRequests = 1
			}
			if requests.Load() != wantRequests {
				t.Fatal("round requests", requests.Load(), wantRequests)
			}
			task, found, err := h.Task(bg, hookTask)
			if err != nil || !found || task.Kind != "pi.tool" || task.Version != 4 || task.Input == nil || task.State.Outcome == nil || task.State.Outcome.Result == nil {
				t.Fatal("hook task ownership", task, found, err)
			}
			input := generationObject(task.Input.Value)
			if input["kind"] != "model" || input["callId"] != "ghost-call" || input["unoffered"] != true {
				t.Fatal("unoffered task input", input)
			}
			receipt := v160ToolResult(t, bg, conversation, "ghost-call")
			if !receipt.IsError || !strings.HasPrefix(v160ToolResultText(receipt), "try another tool<harness>") || len(receipt.Diagnostics) != 1 || receipt.Diagnostics[0].Code != "tool_unavailable" {
				t.Fatal("hook result", receipt)
			}
			result := generationObject(task.State.Outcome.Result.Value)
			entry, found, err := h.Entry(bg, submissionEntryID(result["entryId"]))
			if err != nil || !found || entry.ByTask != hookTask || entry.Kind != "pi.tool-result" {
				t.Fatal("owned result entry", entry, found, err)
			}
		})
	}
}

func TestToolTerminateFinalBoundaryQueuesFollowUpAndPreservesHookControl(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		entered, release := make(chan struct{}), make(chan struct{})
		ext := &Extension{Name: "hook-control", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "end", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "end.test", Version: 1, Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
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
