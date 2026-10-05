package goai

import "testing"

func TestSnapshotEventOwnsMutableProviderPartial(t *testing.T) {
	partial := &Message{Role: RoleAssistant, Content: []ContentBlock{{Type: "toolCall", ID: "call", Name: "tool", Arguments: map[string]any{"nested": map[string]any{"value": "original"}}}}, Usage: &Usage{Input: 1}, Deferred: &DeferredHandle{ID: "job", Data: map[string]any{"state": "original"}}}
	event := SnapshotEvent(&ToolCallDeltaEvent{Partial: partial, Delta: "x"}).(*ToolCallDeltaEvent)
	partial.Content[0].Name = "mutated"
	partial.Content[0].Arguments["nested"].(map[string]any)["value"] = "mutated"
	partial.Usage.Input = 9
	partial.Deferred.Data.(map[string]any)["state"] = "mutated"
	if event.Partial.Content[0].Name != "tool" || event.Partial.Content[0].Arguments["nested"].(map[string]any)["value"] != "original" || event.Partial.Usage.Input != 1 || event.Partial.Deferred.Data.(map[string]any)["state"] != "original" {
		t.Fatal("provider event alias", event)
	}
	terminal := &DoneEvent{Message: partial}
	if SnapshotEvent(terminal) != terminal {
		t.Fatal("terminal ownership unnecessarily copied")
	}
	call := ToolCall{ID: "call", Arguments: map[string]any{"value": "original"}}
	ended := SnapshotEvent(&ToolCallEndEvent{Partial: partial, ToolCall: call}).(*ToolCallEndEvent)
	call.Arguments["value"] = "mutated"
	if ended.ToolCall.Arguments["value"] != "original" {
		t.Fatal("ended tool args alias")
	}
}
