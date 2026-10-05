package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestForegroundSubagentOwnedConversationAndAnswer(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var childID ID
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "subagent", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "subagent.foreground", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			child, err := api.CreateOwnedConversation(ctx, "worker", nil)
			if err != nil {
				t.Error("create child", err)
				return ToolResult{}, err
			}
			repeated, err := api.CreateOwnedConversation(ctx, "worker", nil)
			if err != nil || repeated.ID() != child.ID() {
				t.Error("repeat child", err)
				return ToolResult{}, reject("owned child duplication")
			}
			childID = child.ID()
			sub, err := child.Submit(ctx, Input{Content: "child question", RequestID: "worker-answer"})
			if err != nil {
				t.Error("child submit", err)
				return ToolResult{}, err
			}
			settled, err := sub.Wait(ctx)
			if err != nil {
				t.Error("child wait", err)
				return ToolResult{}, err
			}
			text := ""
			for _, block := range settled.Message.Content {
				if block.Type == "text" {
					text += block.Text
				}
			}
			return ToolResult{Content: text, Details: JSON{"conversationId": child.ID()}}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			number := calls.Add(1)
			if number == 1 {
				ch <- toolAnswer("delegate", "subagent", JSON{})
			} else if number == 2 {
				ch <- terminal("child answer")
			} else {
				found := false
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						for _, block := range message.Content {
							if strings.Contains(block.Text, "child answer") {
								found = true
							}
						}
					}
				}
				if !found {
					t.Error("missing subagent answer", input.Messages)
				}
				ch <- terminal("parent answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		parent := root(t, h, ref)
		sub, err := parent.Submit(bg, Input{Content: "delegate"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		if result.Submission.Status != "done" || calls.Load() != 3 || childID == 0 {
			t.Fatal(result, calls.Load(), childID)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		child, ok := state.Conversations[childID]
		if !ok || child.Owner == 0 || state.Tasks[child.Owner].Kind != "pi.tool" {
			t.Fatal("subagent ownership", child)
		}
		owned := 0
		for _, conversation := range state.Conversations {
			if conversation.Owner == child.Owner {
				owned++
			}
		}
		if owned != 1 {
			t.Fatal("replayed child duplication", owned)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		recovered, err := second.Conversation(bg, childID)
		if err != nil || recovered == nil {
			t.Fatal(err)
		}
		view, err := recovered.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		messages := []MessageReceipt{}
		for _, message := range view.Messages {
			if message.Role != goai.RoleSystem {
				messages = append(messages, message)
			}
		}
		if len(messages) != 2 || messages[0].Role != goai.RoleUser || messages[0].Content[0].Text != "child question" || messages[1].Role != goai.RoleAssistant || messages[1].Content[0].Text != "child answer" || len(ReplayToolDeclarations(view.Messages)) != 1 || calls.Load() != 3 {
			t.Fatal("child persistence with positional loadout", view, calls.Load())
		}
	})
}
