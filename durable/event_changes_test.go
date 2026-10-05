package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"sync"
	"testing"
	"time"
)

func TestPartialMessageChangesPreserveBlocksAndNestedToolArgumentAppends(t *testing.T) {
	before := MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "plan"}, {Type: "text", Text: "a"}, {Type: "toolCall", ID: "call", Name: "echo", Arguments: map[string]any{"text": "x"}}}}
	after, err := detachReceipts([]MessageReceipt{before}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	after[0].Content[0].Thinking = "plan more"
	after[0].Content[1].Text = "abc"
	after[0].Content[2].Arguments["text"] = "xyz"
	changes, err := partialMessageChanges(before, after[0], DefaultLimits())
	if err != nil || len(changes) != 3 {
		t.Fatal(changes, err)
	}
	if changes[0].Type != "thinking_delta" || changes[0].Delta != " more" || changes[1].Type != "text_delta" || changes[1].Delta != "bc" || changes[2].Type != "toolcall_delta" || changes[2].Delta != "yz" || len(changes[2].Path) != 1 || changes[2].Path[0] != "text" {
		t.Fatal(changes)
	}
	after[0].Content[1].Text = "replace"
	changes, err = partialMessageChanges(before, after[0], DefaultLimits())
	if err != nil || changes[1].Type != "block" || changes[1].Block.Text != "replace" {
		t.Fatal(changes, err)
	}
	after[0].Content = after[0].Content[:1]
	changes, err = partialMessageChanges(before, after[0], DefaultLimits())
	if err != nil || len(changes) != 1 || changes[0].Type != "message" {
		t.Fatal(changes, err)
	}
}
func TestAgentEventsActualPartialsAndToolEndAdjacentToResult(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "event.echo", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{Content: "tool receipt"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		partialA, partialB := make(chan struct{}), make(chan struct{})
		calls := 0
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				go func() {
					defer close(ch)
					first := &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "plan"}, {Type: "text", Text: "a"}}}
					ch <- &goai.TextDeltaEvent{Partial: first, ContentIndex: 1, Delta: "a"}
					select {
					case <-partialA:
					case <-ctx.Done():
						return
					}
					second := &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "plan more"}, {Type: "text", Text: "abc"}}}
					ch <- &goai.ThinkingDeltaEvent{Partial: second, ContentIndex: 0, Delta: " more"}
					select {
					case <-partialB:
					case <-ctx.Done():
						return
					}
					ch <- toolAnswer("event-call", "echo", JSON{})
				}()
			} else {
				ch <- terminal("answer")
				close(ch)
			}
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		stream, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Stop()
		var mu sync.Mutex
		all := []AgentEvent{}
		seenUpdate := false
		finished := make(chan struct{}, 1)
		if err := stream.Start(func(_ context.Context, events []AgentEvent) error {
			mu.Lock()
			defer mu.Unlock()
			all = append(all, events...)
			for _, event := range events {
				if event.Type == "message_start" && event.Message != nil && event.Message.Role == goai.RoleAssistant && len(event.Message.Content) == 2 && event.Message.Content[0].Type == "thinking" {
					select {
					case <-partialA:
					default:
						close(partialA)
					}
				}
				if event.Type == "message_update" {
					seenUpdate = true
					if len(event.Changes) != 2 || event.Changes[0].Type != "thinking_delta" || event.Changes[1].Type != "text_delta" {
						t.Error("partial block changes", event.Changes)
					}
					select {
					case <-partialB:
					default:
						close(partialB)
					}
				}
				if event.Type == "run_end" {
					select {
					case finished <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			end, _ := stream.End()
			t.Fatal("event run end missing", end)
		}
		mu.Lock()
		defer mu.Unlock()
		if !seenUpdate {
			t.Fatal("partial update missing")
		}
		matched := false
		for i, event := range all {
			if event.Type == "tool_execution_end" {
				matched = true
				if event.Entry == nil || event.Message == nil || event.Message.ToolCallID != "event-call" || i+2 >= len(all) || all[i+1].Type != "message_start" || all[i+1].Message.Role != goai.RoleToolResult || all[i+2].Type != "entry_appended" || all[i+2].Entry.ID != event.Entry.ID {
					t.Fatal("tool end/result adjacency", i, event, all)
				}
			}
		}
		if !matched {
			t.Fatal("tool end missing")
		}
	})
}
