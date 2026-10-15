package goai_test

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStrict120RequiredConstraintRejectsBeforeHTTP(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAICompletions, goai.ApiOpenAIResponses} {
		for _, supported := range []bool{false, true} {
			t.Run(string(api)+map[bool]string{false: "unsupported", true: "bad-schema"}[supported], func(t *testing.T) {
				var requests atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(400) }))
				defer server.Close()
				tool := goai.Tool{Name: "required", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "require"}}
				if supported {
					tool.Parameters = json.RawMessage(`{"type":"object","oneOf":[{}]}`)
				}
				model := &goai.Model{Provider: goai.ProviderOpenAI, ID: "fixture", Api: api, BaseURL: server.URL, CompletionsCompat: &goai.OpenAICompletionsCompat{SupportsStrictMode: &supported}, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsStrictMode: &supported}}
				found := false
				for event := range goai.Stream(context.Background(), model, &goai.Context{Tools: []goai.Tool{tool}}, &goai.StreamOptions{APIKey: "fixture"}) {
					if e, ok := event.(*goai.ErrorEvent); ok {
						found = true
						if e.Err == nil || !strings.Contains(e.Err.Error(), "requires JSON-schema constrained sampling") {
							t.Error(e.Err)
						}
					}
				}
				if !found || requests.Load() != 0 {
					t.Fatal("strict requirement sent HTTP", found, requests.Load())
				}
			})
		}
	}
}

func TestStrict120PreferredFallbackWire(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAICompletions, goai.ApiOpenAIResponses} {
		for _, supported := range []bool{false, true} {
			t.Run(string(api)+map[bool]string{false: "unsupported", true: "unsupported-schema"}[supported], func(t *testing.T) {
				observed := make(chan map[string]any, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					observed <- body
					w.Header().Set("Content-Type", "text/event-stream")
					if api == goai.ApiOpenAIResponses {
						w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"))
					} else {
						w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
					}
				}))
				defer server.Close()
				schema := json.RawMessage(`{"type":"object","oneOf":[{}]}`)
				tool := goai.Tool{Name: "preferred", Parameters: schema, ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}
				model := &goai.Model{Provider: goai.ProviderOpenAI, ID: "fixture", Api: api, BaseURL: server.URL, CompletionsCompat: &goai.OpenAICompletionsCompat{SupportsStrictMode: &supported}, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsStrictMode: &supported}}
				for event := range goai.Stream(context.Background(), model, &goai.Context{Tools: []goai.Tool{tool}}, &goai.StreamOptions{APIKey: "fixture"}) {
					if e, ok := event.(*goai.ErrorEvent); ok {
						t.Fatal(e.Err)
					}
				}
				body := <-observed
				tools := body["tools"].([]any)
				definition := tools[0].(map[string]any)
				if api == goai.ApiOpenAICompletions {
					definition = definition["function"].(map[string]any)
				}
				if definition["strict"] == true {
					t.Fatal("preferred fallback emitted strict", definition)
				}
				parameters := definition["parameters"].(map[string]any)
				if _, ok := parameters["oneOf"]; !ok {
					t.Fatal("fallback schema changed", parameters)
				}
			})
		}
	}
}
