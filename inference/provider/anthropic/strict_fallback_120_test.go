package anthropic

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnthropic120PreferredStrictLimitFallbackOnce(t *testing.T) {
	for _, mode := range []string{"fallback", "tool-count", "union-count", "second-reject", "unrelated", "required-only"} {
		t.Run(mode, func(t *testing.T) {
			requests := 0
			payloadHooks, responseHooks := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
				}
				if body["max_tokens"] != float64(123) {
					t.Error("payload override lost", body["max_tokens"])
				}
				tools := body["tools"].([]any)
				required := tools[len(tools)-1].(map[string]any)
				if required["strict"] != true {
					t.Error("mandatory strict lost", body)
				}
				if mode != "required-only" {
					prefer := tools[0].(map[string]any)
					if requests == 1 && prefer["strict"] != true {
						t.Error("prefer not initially strict")
					}
					if requests == 2 && prefer["strict"] != nil {
						t.Error("prefer still strict on fallback", prefer)
					}
				}
				if requests == 1 || mode == "second-reject" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(400)
					message := "The compiled grammar is too large"
					if mode == "tool-count" {
						message = "Too many strict tools (21), the limit is 20"
					}
					if mode == "union-count" {
						message = "Schemas contains too many parameters with union types (17), the limit is 16"
					}
					if mode == "unrelated" {
						message = "invalid model"
					}
					json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": message}})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"response\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			}))
			defer server.Close()
			tools := []goai.Tool{{Name: "required", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "require"}}}
			if mode != "required-only" {
				tools = append([]goai.Tool{{Name: "preferred", Parameters: tools[0].Parameters, ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}}, tools...)
			}
			model := strictAnthropicModel()
			model.BaseURL = server.URL
			input := &goai.Context{Tools: tools}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := false
			var message *goai.Message
			for event := range streamAnthropic(ctx, model, input, &goai.StreamOptions{APIKey: "test", OnPayload: func(payload interface{}, _ *goai.Model) (interface{}, error) {
				payloadHooks++
				raw, err := json.Marshal(payload)
				if err != nil {
					return nil, err
				}
				var body map[string]any
				if err := json.Unmarshal(raw, &body); err != nil {
					return nil, err
				}
				body["max_tokens"] = 123
				return body, nil
			}, OnResponse: func(int, map[string]string, *goai.Model) { responseHooks++ }}) {
				if e, ok := event.(*goai.DoneEvent); ok {
					done = true
					message = e.Message
				}
			}
			want := 2
			if mode == "unrelated" || mode == "required-only" {
				want = 1
			}
			success := mode == "fallback" || mode == "tool-count" || mode == "union-count"
			if payloadHooks != want || responseHooks != 1 {
				t.Error("fallback hook counts", payloadHooks, responseHooks, want)
			}
			if requests != want || done != success {
				t.Fatal(mode, requests, done)
			}
			if done && (message == nil || len(message.Diagnostics) != 1 || message.Diagnostics[0].Type != "anthropic_strict_tools_fallback") {
				t.Fatal("fallback diagnostic absent", message)
			}
			if mode != "required-only" && input.Tools[0].ConstrainedSampling.Strict != "prefer" {
				t.Fatal("input mutated")
			}
		})
	}
}
