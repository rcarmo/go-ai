package openairesponses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func responsesHistoryTool() goai.Tool {
	return goai.Tool{Name: "script", Parameters: json.RawMessage(`{"type":"object","required":["payload"],"properties":{"payload":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
}
func TestResponsesHistoricalGrammarPreflightMain(t *testing.T) {
	good := responsesHistoryTool()
	badSchema := good
	badSchema.Parameters = json.RawMessage(`{"type":"object","required":["payload"],"properties":{"payload":{"type":"number"}}}`)
	badVariant := responsesHistoryTool()
	badVariant.ConstrainedSampling.Variants = nil
	cases := []struct {
		name                             string
		tools                            []goai.Tool
		value                            any
		replay, unsupported, wantFailure bool
	}{{name: "schema", tools: []goai.Tool{badSchema}, wantFailure: true}, {name: "variant", tools: []goai.Tool{badVariant}, wantFailure: true}, {name: "replaced", tools: []goai.Tool{badSchema, good}}, {name: "unsupported", tools: []goai.Tool{badSchema}, unsupported: true}, {name: "number", tools: []goai.Tool{good}, replay: true, value: 42, wantFailure: true}, {name: "missing", tools: []goai.Tool{good}, replay: true, wantFailure: true}, {name: "string", tools: []goai.Tool{good}, replay: true, value: "literal"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{}}}\n\n"))
			}))
			defer server.Close()
			yes := !tc.unsupported
			model := &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, BaseURL: server.URL, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsOpenAIGrammarTools: &yes}}
			input := &goai.Context{}
			for _, tool := range tc.tools {
				input.Messages = append(input.Messages, goai.Message{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{tool}})
			}
			input.Messages = append(input.Messages, goai.Message{Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "script"}}})
			if tc.replay {
				input.Messages = append(input.Messages, goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "c|ctc_i", Name: "script", Arguments: map[string]any{"payload": tc.value}}}})
			}
			failures := 0
			for event := range streamResponses(context.Background(), model, input, &goai.StreamOptions{APIKey: "fixture"}) {
				if e, ok := event.(*goai.ErrorEvent); ok {
					failures++
					if !tc.wantFailure || !strings.Contains(e.Err.Error(), "grammar") {
						t.Error(e.Err)
					}
				}
			}
			if tc.wantFailure {
				if failures != 1 || requests.Load() != 0 {
					t.Fatal("bad history reached transport", failures, requests.Load())
				}
			} else if failures != 0 || requests.Load() != 1 {
				t.Fatal("valid history rejected", failures, requests.Load())
			}
		})
	}
}
func TestResponsesNativeGrammarValidationMain(t *testing.T) {
	value, err := jsonparse.ParseJSONValue(`"\udfff"`, true)
	if err != nil {
		t.Fatal(err)
	}
	block := goai.ContentBlock{Type: "toolCall", Name: "script", Arguments: map[string]any{"payload": value}}
	input := &goai.Context{Tools: []goai.Tool{responsesHistoryTool()}, Messages: []goai.Message{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{block}}}}
	if validateResponsesGrammarContext(input, true) == nil {
		t.Fatal("unowned scalar accepted")
	}
	input.Messages[0].Content[0].SetStreamingArguments(block.Arguments, "")
	if err = validateResponsesGrammarContext(input, true); err != nil {
		t.Fatal(err)
	}
	text, ok := responsesGrammarInput(input.Messages[0].Content[0], "payload")
	if !ok || text != value.(goai.UTF16String).WTF8() {
		t.Fatal("owned scalar changed")
	}
}
