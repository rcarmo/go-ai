package openairesponses

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0843AzureOpenAIToolChoiceSerializes(t *testing.T) {
	model := &goai.Model{ID: "deployment", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{{Name: "read", Description: "Read", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}}}
	req := buildRequest(model, ctx, &goai.StreamOptions{ToolChoice: goai.ToolChoiceRequired})
	if req.ToolChoice != "required" {
		t.Fatalf("tool_choice=%q, want required", req.ToolChoice)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "read" {
		t.Fatalf("tools not preserved: %#v", req.Tools)
	}
}
