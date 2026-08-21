package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0842ResponsesNamespaceRoundTripAndEndTurn(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	stream := strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"fc_test","call_id":"call_test","name":"lookup","arguments":""}}`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_test","call_id":"call_test","name":"lookup","arguments":"{\"value\":\"hello\"}","namespace":"dynamic_tools"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_test","status":"completed","end_turn":false,"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		`data: [DONE]`,
	}, "\n\n")
	ch := make(chan goai.Event, 16)
	processStream(strings.NewReader(stream), model, ch)
	close(ch)

	var done *goai.Message
	for event := range ch {
		if e, ok := event.(*goai.DoneEvent); ok {
			done = e.Message
		}
	}
	if done == nil {
		t.Fatal("missing done event")
	}
	if done.EndTurn == nil || *done.EndTurn {
		t.Fatalf("expected endTurn=false, got %#v", done.EndTurn)
	}
	if len(done.Content) != 1 || done.Content[0].Namespace != "dynamic_tools" {
		t.Fatalf("namespace not captured: %#v", done.Content)
	}
	items := buildAssistantItems(0, *done, model)
	call := items[0].(map[string]interface{})
	if call["namespace"] != "dynamic_tools" {
		t.Fatalf("namespace not replayed: %#v", call)
	}
}

func TestV0842ResponsesAdditionalToolsMode(t *testing.T) {
	yes := true
	model := &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsAdditionalTools: &yes, SupportsStrictMode: &yes}}
	ctx := deferredContext([]goai.Tool{deferredTool("base_tool"), deferredTool("late_tool")}, []string{"late_tool"})
	req := buildRequest(model, ctx, &goai.StreamOptions{})
	if len(req.Tools) != 1 || req.Tools[0].Name != "base_tool" {
		t.Fatalf("tools=%#v, want only base_tool immediate", req.Tools)
	}
	var input []map[string]interface{}
	if err := json.Unmarshal(req.Input, &input); err != nil {
		t.Fatal(err)
	}
	var foundAdditional bool
	for _, item := range input {
		if item["type"] == "tool_search_output" {
			t.Fatalf("additional_tools mode should not emit tool_search_output: %#v", input)
		}
		if item["type"] == "additional_tools" {
			foundAdditional = true
			tools := item["tools"].([]interface{})
			tool := tools[0].(map[string]interface{})
			if tool["name"] != "late_tool" || tool["defer_loading"] != nil {
				t.Fatalf("unexpected additional_tools payload: %#v", item)
			}
		}
	}
	if !foundAdditional {
		t.Fatalf("additional_tools item not found in %#v", input)
	}
}
