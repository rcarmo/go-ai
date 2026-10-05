package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestToolExecutionParallelAndSequentialRounds(t *testing.T) {
	for _, mode := range []string{"parallel", "sequential", "tool-sequential"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				firstEntered, secondEntered, releaseFirst := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var firstReturned atomic.Bool
				schema := json.RawMessage(`{"type":"object"}`)
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "first", Parameters: schema}, Implementation: "round.first", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					close(firstEntered)
					<-releaseFirst
					firstReturned.Store(true)
					return ToolResult{Content: "first result"}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				toolMode := ""
				if mode == "tool-sequential" {
					toolMode = "sequential"
				}
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "second", Parameters: schema}, Implementation: "round.second", Version: 1, ExecutionMode: toolMode, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					if mode != "parallel" && !firstReturned.Load() {
						t.Error("sequential tool overlapped")
					}
					close(secondEntered)
					return ToolResult{Content: "second result"}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if calls.Add(1) == 1 {
						answer := toolAnswer("first-call", "first", JSON{})
						answer.Message.Content = append(answer.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "second-call", Name: "second", Arguments: map[string]any{}})
						ch <- answer
					} else {
						order := []string{}
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								order = append(order, message.ToolCallID)
							}
						}
						if fmt.Sprint(order) != "[first-call second-call]" {
							t.Error("tool result context order", order)
						}
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, releaseFirst)
				setting := mode
				if mode == "tool-sequential" {
					setting = "parallel"
				}
				conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{ToolExecution: setting}})
				if err != nil {
					t.Fatal(err)
				}
				sub, err := conversation.Submit(bg, Input{Content: "round"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, firstEntered)
				if mode == "parallel" {
					awaitTaskSignal(t, secondEntered)
				}
				releaseTaskGate(releaseFirst)
				awaitTaskSignal(t, secondEntered)
				result := waitSubmission(t, sub)
				if result.Submission.Status != "done" || calls.Load() != 2 {
					t.Fatal(result, calls.Load())
				}
			})
		})
	}
}
