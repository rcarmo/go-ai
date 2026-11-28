package openairesponses

import (
	"context"
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAzureOpenAIToolChoiceUpstreamRequiredAndNonePreserveTools(t *testing.T) {
	for _, tc := range []struct {
		name   string
		choice goai.ToolChoice
		simple bool
	}{
		{name: "provider-specific required", choice: goai.ToolChoiceRequired},
		{name: "simple none", choice: goai.ToolChoiceNone, simple: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &azureCaptureTransport{}
			model := &goai.Model{ID: "test-deployment", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
			ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Summarize this")}, Tools: []goai.Tool{{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}}}
			opts := &goai.StreamOptions{APIKey: "test-key", AzureBaseURL: "https://resource.ai.azure.com", ToolChoice: tc.choice, HTTPTransport: transport}
			stream := streamResponses
			if tc.simple {
				stream = streamResponsesSimple
			}
			for event := range stream(context.Background(), model, ctx, opts) {
				if failure, ok := event.(*goai.ErrorEvent); ok {
					t.Fatal(failure.Err)
				}
			}
			var payload responsesRequest
			if err := json.NewDecoder(transport.request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ToolChoice != string(tc.choice) {
				t.Fatalf("tool_choice = %q, want %q", payload.ToolChoice, tc.choice)
			}
			if len(payload.Tools) != 1 || payload.Tools[0].Name != "read" {
				t.Fatalf("tools not preserved: %#v", payload.Tools)
			}
		})
	}
}

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
