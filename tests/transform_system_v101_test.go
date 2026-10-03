package goai_test

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"testing"
)

func TestV101TransformPreservesSystemToolDeltaWithoutOrphanFlush(t *testing.T) {
	model := &goai.Model{ID: "m", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic}
	delta := goai.Message{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "update"}}, ToolsAdded: []goai.Tool{{Name: "late", Parameters: json.RawMessage(`{"type":"object"}`)}}, ToolsRemoved: []goai.ToolReference{{Name: "old"}}}
	input := []goai.Message{{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call", Name: "old", Arguments: map[string]any{}}}}, delta, {Role: goai.RoleToolResult, ToolCallID: "call", ToolName: "old", Content: []goai.ContentBlock{{Type: "text", Text: "result"}}}, goai.UserMessage("continue")}
	out := goai.TransformMessages(input, model)
	if len(out) != 4 || !reflect.DeepEqual(out[1], delta) || out[2].IsError {
		t.Fatalf("out=%#v", out)
	}
}
