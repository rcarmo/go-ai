package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestToolControlsPinnedUnanimousTerminationLastHandoffAndAddTools(t *testing.T) {
	for _, mode := range []string{"terminate", "one-terminate", "handoff", "add-tools"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var effects atomic.Int64
				for index, name := range []string{"first", "second", "extra"} {
					index, name := index, name
					if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "control." + name, Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
						effects.Add(1)
						control := &ToolControl{}
						switch mode {
						case "terminate":
							control.Terminate = true
						case "one-terminate":
							control.Terminate = index == 0
						case "handoff":
							value := fmt.Sprintf("handoff%d", index)
							control.Handoff = &value
						case "add-tools":
							control.AddTools = []string{"extra", "extra"}
						}
						return ToolResult{Content: name, Control: control}, nil
					}}); err != nil {
						t.Fatal(err)
					}
				}
				var calls atomic.Int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					n := calls.Add(1)
					ch := make(chan goai.Event, 1)
					if n == 1 {
						answer := terminal("calls")
						answer.Message.StopReason = goai.StopReasonToolUse
						answer.Message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "first-call", Name: "first", Arguments: map[string]any{}}, {Type: "toolCall", ID: "second-call", Name: "second", Arguments: map[string]any{}}}
						answer.Reason = goai.StopReasonToolUse
						ch <- answer
					} else {
						if mode == "terminate" || mode == "handoff" {
							t.Error("terminal control generated again")
						}
						if mode == "add-tools" {
							found := false
							for _, tool := range input.Tools {
								if tool.Name == "extra" {
									found = true
								}
							}
							if !found {
								t.Error("added tool missing next request")
							}
						}
						ch <- terminal("answer")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				selection := []string{"first", "second"}
				c, err := h.Root(bg, AgentChange{Model: ref, Tools: &selection})
				if err != nil {
					t.Fatal(err)
				}
				sub, err := c.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				settled := waitSubmission(t, sub)
				if settled.Submission.Status != "done" || effects.Load() != 2 {
					t.Fatal("control settlement", settled, effects.Load())
				}
				wantCalls := int64(2)
				if mode == "terminate" || mode == "handoff" {
					wantCalls = 1
				}
				if calls.Load() != wantCalls {
					t.Fatal("control model calls", calls.Load())
				}
				view, err := c.ContextView(bg, 0)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "handoff" {
					if view.Head == nil || len(view.Messages) != 1 || view.Messages[0].Content[0].Text != "handoff1" {
						t.Fatal("last handoff/reset", view)
					}
				}
				if mode == "add-tools" {
					snapshot, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					doc, _ := agentDocument(snapshot, c.ID())
					var agent agentState
					if err := fromObject(doc.Value, &agent, h.session.limits); err != nil {
						t.Fatal(err)
					}
					if agent.Tools == nil || len(*agent.Tools) != 3 {
						t.Fatal("add tools duplicate or missing", agent.Tools)
					}
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
				recovered, err := second.Submission(bg, sub.ID())
				if err != nil {
					t.Fatal(err)
				}
				waitSubmission(t, recovered)
				if calls.Load() != wantCalls {
					t.Fatal("control recovery regenerated")
				}
			})
		})
	}
}
