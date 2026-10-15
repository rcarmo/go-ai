package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnthropic120StrictFallbackBoundaries(t *testing.T) {
	for _, mode := range []string{"wrong-type", "wrong-status", "cancel-before-retry", "hook-error", "sse-error", "later-request"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			requests := 0
			strictFlags := []bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				tool := body["tools"].([]any)[0].(map[string]any)
				strictFlags = append(strictFlags, tool["strict"] == true)
				if mode == "sse-error" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"The compiled grammar is too large\"}}\n\n"))
					return
				}
				if mode == "wrong-type" || requests == 1 {
					w.Header().Set("Content-Type", "application/json")
					status := 400
					if mode == "wrong-status" {
						status = 500
					}
					w.WriteHeader(status)
					if mode == "cancel-before-retry" {
						cancel()
					}
					kind := "invalid_request_error"
					if mode == "wrong-type" {
						kind = "authentication_error"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": kind, "message": "The compiled grammar is too large"}})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"ok\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			}))
			defer server.Close()
			model := strictAnthropicModel()
			model.BaseURL = server.URL
			input := &goai.Context{Tools: []goai.Tool{{Name: "preferred", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}}}
			options := &goai.StreamOptions{APIKey: "fixture", RetryConfig: &goai.RetryConfig{MaxRetries: 0}}
			if mode == "hook-error" {
				options.OnPayload = func(any, *goai.Model) (any, error) {
					return nil, errors.New("400 invalid_request_error: The compiled grammar is too large")
				}
			}
			runs := 1
			if mode == "later-request" {
				runs = 2
			}
			for i := 0; i < runs; i++ {
				done, failed := false, false
				for event := range streamAnthropic(ctx, model, input, options) {
					switch event.(type) {
					case *goai.DoneEvent:
						done = true
					case *goai.ErrorEvent:
						failed = true
					}
				}
				if mode == "later-request" {
					if !done || failed {
						t.Fatal("fallback success missing")
					}
				} else if !failed || done {
					t.Fatal("failure boundary not preserved", mode)
				}
			}
			want := 1
			if mode == "hook-error" {
				want = 0
			}
			if mode == "later-request" {
				want = 3
			}
			if requests != want {
				t.Fatal("unexpected retry", mode, requests)
			}
			if mode == "later-request" && (!strictFlags[0] || strictFlags[1] || !strictFlags[2]) {
				t.Fatal("fallback leaked to later request", strictFlags)
			}
		})
	}
}
