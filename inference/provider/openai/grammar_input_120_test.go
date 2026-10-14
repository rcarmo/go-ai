package openai

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestGrammar120InputPropertyStreamAndReplay(t *testing.T) {
	tool := goai.Tool{Name: "script", Parameters: json.RawMessage(`{"type":"object","required":["code"],"properties":{"code":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
	body := strings.NewReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call\",\"type\":\"custom\",\"custom\":{\"name\":\"script\",\"input\":\"a\\n\"}}]}}]}\n\n" + "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"custom\":{\"input\":\"b\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
	var done *goai.DoneEvent
	deltas := ""
	processSSEStreamWithOptions(body, &goai.Model{}, nil, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.ToolCallDeltaEvent:
			deltas += e.Delta
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Error(e.Err)
		}
	}, []goai.Tool{tool})
	if done == nil || done.Message.Content[0].Arguments["code"] != "a\nb" {
		t.Fatal(done)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(deltas), &args); err != nil || args["code"] != "a\nb" {
		t.Fatal(deltas, err)
	}
	yes := true
	compat := goai.OpenAICompletionsCompat{SupportsOpenAIGrammarTools: &yes}
	messages := convertMessages(&goai.Model{}, &goai.Context{Messages: []goai.Message{*done.Message}}, &compat, []goai.Tool{tool})
	bytes, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bytes), `"type":"custom"`) || strings.Contains(string(bytes), `"function"`) {
		t.Fatal(string(bytes))
	}
	compat.SupportsOpenAIGrammarTools = nil
	messages = convertMessages(&goai.Model{}, &goai.Context{Messages: []goai.Message{*done.Message}}, &compat, []goai.Tool{tool})
	if messages[0].ToolCalls[0].Type != "function" {
		t.Fatal(messages)
	}
}
func TestGrammar120RejectsAmbiguousInputSchema(t *testing.T) {
	for _, schema := range []string{`{}`, `{"type":"object","properties":{"code":{"type":"string"}}}`, `{"type":"object","required":["code","other"],"properties":{"code":{"type":"string"}}}`, `{"type":"object","required":["code"],"properties":{"code":{"type":"number"}}}`} {
		tool := goai.Tool{Name: "bad", Parameters: json.RawMessage(schema), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
		if _, err := openAIGrammarToolDef(tool, nil); err == nil {
			t.Fatal(schema)
		}
	}
}

func TestGrammar120EmptyInputDelta(t *testing.T) {
	delta, err := appendGrammarInputDelta("", "", true, "code")
	var args map[string]string
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(delta), &args); err != nil || args["code"] != "" || len(args) != 1 {
		t.Fatal(delta, err)
	}
}
func TestGrammar120RejectsMalformedReplayBeforeHTTP(t *testing.T) {
	yes := true
	tool := goai.Tool{Name: "script", Parameters: json.RawMessage(`{"type":"object","required":["code"],"properties":{"code":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
	for _, args := range []map[string]any{{}, {"code": 12}} {
		events := streamOpenAI(context.Background(), &goai.Model{Provider: goai.ProviderOpenAI, BaseURL: "http://127.0.0.1:1", CompletionsCompat: &goai.OpenAICompletionsCompat{SupportsOpenAIGrammarTools: &yes}}, &goai.Context{Tools: []goai.Tool{tool}, Messages: []goai.Message{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", Name: "script", Arguments: args}}}}}, &goai.StreamOptions{APIKey: "fixture"})
		found := false
		for event := range events {
			if e, ok := event.(*goai.ErrorEvent); ok {
				found = true
				if !strings.Contains(e.Err.Error(), "requires string argument") {
					t.Fatal(e.Err)
				}
			}
		}
		if !found {
			t.Fatal("malformed replay accepted")
		}
	}
}
