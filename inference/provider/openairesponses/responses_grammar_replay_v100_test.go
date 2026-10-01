package openairesponses

import (
	"context"
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func grammarReplayModel() *goai.Model {
	return &goai.Model{
		ID:            "gpt-test",
		Provider:      goai.ProviderOpenAI,
		Api:           goai.ApiOpenAIResponses,
		BaseURL:       "https://api.openai.com/v1",
		Input:         []string{"text"},
		ContextWindow: 100000,
		MaxTokens:     4096,
		ResponsesCompat: &goai.OpenAIResponsesCompat{
			SupportsOpenAIGrammarTools: boolPtr(true),
		},
	}
}

func grammarReplayTool() goai.Tool {
	return goai.Tool{
		Name:        "grammar_tool",
		Description: "grammar",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{
			Type:     "grammar",
			Variants: map[string]string{"openai_lark": "start: /[a-z]+/"},
		},
	}
}

func captureResponsesInputForReplay(t *testing.T, msg goai.Message, tools []goai.Tool) []map[string]interface{} {
	t.Helper()
	model := grammarReplayModel()
	var input []map[string]interface{}
	ctx := &goai.Context{Messages: []goai.Message{msg}, Tools: tools}
	for event := range streamResponses(context.Background(), model, ctx, &goai.StreamOptions{
		APIKey: "test-key",
		OnPayload: func(payload interface{}, model *goai.Model) (interface{}, error) {
			request, ok := payload.(responsesRequest)
			if !ok {
				t.Fatalf("payload type %T", payload)
			}
			if err := json.Unmarshal(request.Input, &input); err != nil {
				t.Fatalf("decode input: %v", err)
			}
			return nil, context.Canceled
		},
	}) {
		if _, ok := event.(*goai.ErrorEvent); ok {
			break
		}
	}
	if len(input) == 0 {
		t.Fatal("no input captured")
	}
	return input
}

func TestResponsesGrammarReplayUsesCustomToolCallAndDropsMismatchedItemID(t *testing.T) {
	msg := goai.Message{
		Role:     goai.RoleAssistant,
		Api:      goai.ApiOpenAIResponses,
		Provider: goai.ProviderOpenAI,
		Model:    "gpt-test",
		Content: []goai.ContentBlock{{
			Type:      "toolCall",
			ID:        "call_1|fc_original",
			Name:      "grammar_tool",
			Arguments: map[string]interface{}{"input": "abc"},
		}},
	}
	input := captureResponsesInputForReplay(t, msg, []goai.Tool{grammarReplayTool()})
	item := input[0]
	if item["type"] != "custom_tool_call" || item["call_id"] != "call_1" || item["name"] != "grammar_tool" || item["input"] != "abc" {
		t.Fatalf("unexpected grammar replay item: %#v", item)
	}
	if _, ok := item["id"]; ok {
		t.Fatalf("mismatched fc_ item id must be omitted for custom_tool_call: %#v", item)
	}
}

func TestResponsesReplayKeepsOnlyMatchingItemIDPrefixes(t *testing.T) {
	functionMsg := goai.Message{Role: goai.RoleAssistant, Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI, Model: "gpt-test", Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_2|fc_match", Name: "fn", Arguments: map[string]interface{}{"value": "ok"}}}}
	functionInput := captureResponsesInputForReplay(t, functionMsg, []goai.Tool{{Name: "fn", Parameters: json.RawMessage(`{"type":"object"}`)}})
	if functionInput[0]["type"] != "function_call" || functionInput[0]["id"] != "fc_match" {
		t.Fatalf("expected matching fc_ id on function_call, got %#v", functionInput[0])
	}

	customMsg := goai.Message{Role: goai.RoleAssistant, Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI, Model: "gpt-test", Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_3|ctc_match", Name: "grammar_tool", Arguments: map[string]interface{}{"input": "abc"}}}}
	customInput := captureResponsesInputForReplay(t, customMsg, []goai.Tool{grammarReplayTool()})
	if customInput[0]["type"] != "custom_tool_call" || customInput[0]["id"] != "ctc_match" {
		t.Fatalf("expected matching ctc_ id on custom_tool_call, got %#v", customInput[0])
	}

	mismatchMsg := functionMsg
	mismatchMsg.Content[0].ID = "call_4|ctc_wrong"
	mismatch := captureResponsesInputForReplay(t, mismatchMsg, []goai.Tool{{Name: "fn", Parameters: json.RawMessage(`{"type":"object"}`)}})
	if _, ok := mismatch[0]["id"]; ok {
		t.Fatalf("mismatched ctc_ item id must be omitted for function_call: %#v", mismatch[0])
	}
}

func TestResponsesReplayOmitsItemIDForDifferentModelMessages(t *testing.T) {
	msg := goai.Message{Role: goai.RoleAssistant, Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI, Model: "other-model", Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_5|fc_match", Name: "fn", Arguments: map[string]interface{}{"value": "ok"}}}}
	input := captureResponsesInputForReplay(t, msg, []goai.Tool{{Name: "fn", Parameters: json.RawMessage(`{"type":"object"}`)}})
	if _, ok := input[0]["id"]; ok {
		t.Fatalf("different-model item id must be omitted: %#v", input[0])
	}
}

func captureV100ResponsesReplayRequest(t *testing.T, model *goai.Model, transcript *goai.Context) (responsesRequest, []map[string]interface{}) {
	t.Helper()
	var captured responsesRequest
	var input []map[string]interface{}
	for range streamResponses(t.Context(), model, transcript, &goai.StreamOptions{
		APIKey: "fixture-key",
		OnPayload: func(payload interface{}, _ *goai.Model) (interface{}, error) {
			captured = payload.(responsesRequest)
			if err := json.Unmarshal(captured.Input, &input); err != nil {
				t.Error(err)
			}
			return nil, context.Canceled
		},
	}) {
	}
	if len(input) == 0 {
		t.Fatal("no production input captured")
	}
	return captured, input
}

func TestResponsesGrammarCapabilityControlsDeclarationCallAndResult(t *testing.T) {
	for _, capability := range []string{"default unsupported", "explicit false", "true"} {
		t.Run(capability, func(t *testing.T) {
			model := grammarReplayModel()
			switch capability {
			case "default unsupported":
				model.ResponsesCompat = nil
			case "explicit false":
				model.ResponsesCompat.SupportsOpenAIGrammarTools = boolPtr(false)
			}
			transcript := &goai.Context{Tools: []goai.Tool{grammarReplayTool()}, Messages: []goai.Message{
				{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_roundtrip|ctc_same", Name: "grammar_tool", Arguments: map[string]interface{}{"input": "abc"}}}},
				{Role: goai.RoleToolResult, ToolName: "grammar_tool", ToolCallID: "call_roundtrip|ctc_same", Content: []goai.ContentBlock{{Type: "text", Text: "result"}}},
			}}
			request, input := captureV100ResponsesReplayRequest(t, model, transcript)
			wantDeclaration, wantCall, wantResult := "function", "function_call", "function_call_output"
			if capability == "true" {
				wantDeclaration, wantCall, wantResult = "custom", "custom_tool_call", "custom_tool_call_output"
			}
			if len(request.Tools) != 1 || request.Tools[0].Type != wantDeclaration {
				t.Fatalf("declaration=%#v", request.Tools)
			}
			if len(input) != 2 || input[0]["type"] != wantCall || input[1]["type"] != wantResult || input[0]["call_id"] != "call_roundtrip" || input[1]["call_id"] != "call_roundtrip" || input[1]["output"] != "result" {
				t.Fatalf("call+result=%#v", input)
			}
			if capability == "true" {
				if input[0]["input"] != "abc" || input[0]["id"] != "ctc_same" {
					t.Fatalf("custom=%v", input[0])
				}
			} else if _, ok := input[0]["id"]; ok {
				t.Fatalf("function retained custom id: %v", input[0])
			}
		})
	}
}

func TestResponsesGrammarForeignHistoryOmitsAllItemIDs(t *testing.T) {
	for _, source := range []string{"foreign provider", "foreign API", "same source"} {
		for _, id := range []string{"foreign.raw/123", "fc_foreign", "ctc_valid"} {
			t.Run(source+"/"+id, func(t *testing.T) {
				model := grammarReplayModel()
				provider, api := model.Provider, model.Api
				if source == "foreign provider" {
					provider = goai.ProviderGitHubCopilot
				}
				if source == "foreign API" {
					api = goai.ApiOpenAICompletions
				}
				transcript := &goai.Context{Tools: []goai.Tool{grammarReplayTool()}, Messages: []goai.Message{
					{Role: goai.RoleAssistant, Provider: provider, Api: api, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_1|" + id, Name: "grammar_tool", Arguments: map[string]interface{}{"input": "abc"}}}},
					{Role: goai.RoleToolResult, ToolName: "grammar_tool", ToolCallID: "call_1|" + id, Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}},
				}}
				_, input := captureV100ResponsesReplayRequest(t, model, transcript)
				if input[0]["type"] != "custom_tool_call" || input[0]["call_id"] != "call_1" {
					t.Fatalf("item=%v", input[0])
				}
				if source == "same source" && id == "ctc_valid" {
					if input[0]["id"] != id {
						t.Fatalf("same-origin custom id=%v", input[0])
					}
				} else if _, ok := input[0]["id"]; ok {
					t.Fatalf("unexpected replay id: %v", input[0])
				}
			})
		}
	}
}

func TestResponsesGrammarMissingAndNullInputReplayAsEmpty(t *testing.T) {
	for _, arguments := range []map[string]interface{}{nil, {}, {"input": nil}} {
		model := grammarReplayModel()
		transcript := &goai.Context{Tools: []goai.Tool{grammarReplayTool()}, Messages: []goai.Message{{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_empty|ctc_empty", Name: "grammar_tool", Arguments: arguments}}}}}
		_, input := captureV100ResponsesReplayRequest(t, model, transcript)
		if input[0]["type"] != "custom_tool_call" || input[0]["input"] != "" {
			t.Fatalf("input=%v", input[0])
		}
	}
}

func TestResponsesGrammarReplayResolvesTranscriptDeclaredTools(t *testing.T) {
	model := grammarReplayModel()
	model.ResponsesCompat.SupportsMidConvoSystemMessages = boolPtr(true)
	model.ResponsesCompat.SupportsAdditionalTools = boolPtr(true)
	transcript := &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{grammarReplayTool()}},
		{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_added|ctc_added", Name: "grammar_tool", Arguments: map[string]interface{}{"input": "abc"}}}},
		{Role: goai.RoleToolResult, ToolName: "grammar_tool", ToolCallID: "call_added|ctc_added", Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}},
	}}
	request, input := captureV100ResponsesReplayRequest(t, model, transcript)
	if len(request.Tools) != 1 || request.Tools[0].Type != "custom" {
		t.Fatalf("tools=%v", request.Tools)
	}
	var call, result map[string]interface{}
	for _, item := range input {
		if item["type"] == "custom_tool_call" {
			call = item
		}
		if item["type"] == "custom_tool_call_output" {
			result = item
		}
	}
	if call == nil || result == nil || call["input"] != "abc" || call["id"] != "ctc_added" || result["call_id"] != "call_added" {
		t.Fatalf("input=%v", input)
	}
}
