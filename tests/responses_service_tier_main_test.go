package goai_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

var tierResponsesProvider = goai.GetApiProvider(goai.ApiOpenAIResponses)
var tierCodexProvider = goai.GetApiProvider(goai.ApiOpenAICodexResponses)

func TestResponsesServiceTierAccountingWireMain(t *testing.T) {
	cases := []struct {
		name, request, response string
		present, null           bool
		responses, codex        float64
	}{
		{"missing", "priority", "", false, false, 2, 2},
		{"default", "priority", "default", true, false, 1, 2},
		{"empty", "priority", "", true, false, 1, 1},
		{"null", "flex", "", true, true, .5, .5},
		{"override", "flex", "priority", true, false, 2, 2},
		{"fast_request", "fast", "", false, false, 2, 1},
		{"fast_response", "flex", "fast", true, false, 2, 1},
		{"flex_override", "priority", "flex", true, false, .5, .5},
		{"unknown", "priority", "other", true, false, 1, 1},
	}
	for _, provider := range []*goai.ApiProvider{tierResponsesProvider, tierCodexProvider} {
		if provider == nil {
			t.Fatal("unregistered provider")
		}
		transports := []goai.Transport{goai.TransportSSE}
		if provider.Api == goai.ApiOpenAICodexResponses {
			transports = append(transports, goai.TransportWebSocket)
		}
		for _, transport := range transports {
			for _, id := range []string{"gpt-5.4", "gpt-5.5", "gpt-5.5-2026-10-01"} {
				for _, tc := range cases {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", provider.Api, transport, id, tc.name), func(t *testing.T) {
						reasoningTokens := 7
						if id == "gpt-5.5" {
							reasoningTokens = 0
						}
						response := map[string]any{"id": "r", "status": "completed", "usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120, "input_tokens_details": map[string]any{"cached_tokens": 20, "cache_write_tokens": 10}, "output_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens}}}
						if tc.present {
							if tc.null {
								response["service_tier"] = nil
							} else {
								response["service_tier"] = tc.response
							}
						}
						frame, _ := json.Marshal(map[string]any{"type": "response.completed", "response": response})
						captured := make(chan map[string]any, 1)
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if transport == goai.TransportWebSocket {
								conn, err := websocket.Accept(w, r, nil)
								if err != nil {
									t.Error(err)
									return
								}
								defer conn.CloseNow()
								ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
								defer cancel()
								_, raw, err := conn.Read(ctx)
								if err != nil {
									t.Error(err)
									return
								}
								var body map[string]any
								if err = json.Unmarshal(raw, &body); err != nil {
									t.Error(err)
									return
								}
								captured <- body
								_ = conn.Write(ctx, websocket.MessageText, frame)
								_, _, _ = conn.Read(ctx)
								return
							}
							var reader io.Reader = r.Body
							if r.Header.Get("Content-Encoding") == "zstd" {
								decoder, err := zstd.NewReader(r.Body)
								if err != nil {
									t.Error(err)
									return
								}
								defer decoder.Close()
								reader = decoder
							}
							var body map[string]any
							if err := json.NewDecoder(reader).Decode(&body); err != nil {
								t.Error(err)
								return
							}
							captured <- body
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprintf(w, "data: %s\n\n", frame)
						}))
						defer server.Close()
						model := &goai.Model{ID: id, Api: provider.Api, Provider: goai.ProviderOpenAI, BaseURL: server.URL, Cost: goai.ModelCost{Input: 1, Output: 2, CacheRead: .5, CacheWrite: 1.5}}
						token := "test"
						if provider.Api == goai.ApiOpenAICodexResponses {
							token = "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct"}}`)) + ".x"
						}
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						var done *goai.Message
						opts := &goai.StreamOptions{APIKey: token, Transport: transport, ServiceTier: tc.request}
						for event := range provider.Stream(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
							switch e := event.(type) {
							case *goai.ErrorEvent:
								t.Error(e.Err)
							case *goai.DoneEvent:
								if done != nil {
									t.Error("duplicate completion")
								}
								done = e.Message
							}
						}
						if done == nil || done.Usage == nil {
							t.Fatal("missing usage completion", done)
						}
						var body map[string]any
						select {
						case body = <-captured:
						case <-ctx.Done():
							t.Fatal("missing request")
						}
						if body["service_tier"] != tc.request || opts.ServiceTier != tc.request {
							t.Fatal("request tier lost/mutated", body, opts.ServiceTier)
						}
						multiplier := tc.responses
						if provider.Api == goai.ApiOpenAICodexResponses {
							multiplier = tc.codex
						}
						if multiplier == 2 && id == "gpt-5.5" {
							multiplier = 2.5
						}
						usage := done.Usage
						if usage.Input != 70 || usage.Output != 20 || usage.CacheRead != 20 || usage.CacheWrite != 10 || usage.TotalTokens != 120 || usage.Reasoning != reasoningTokens || !usage.ReasoningSet {
							t.Fatal("tier changed token counters", usage)
						}
						want := []float64{70.0 / 1e6 * multiplier, 40.0 / 1e6 * multiplier, 10.0 / 1e6 * multiplier, 15.0 / 1e6 * multiplier, 135.0 / 1e6 * multiplier}
						got := []float64{usage.Cost.Input, usage.Cost.Output, usage.Cost.CacheRead, usage.Cost.CacheWrite, usage.Cost.Total}
						for index, value := range got {
							tolerance := 16 * (math.Nextafter(want[index], math.Inf(1)) - want[index])
							if math.Abs(value-want[index]) > tolerance {
								t.Errorf("cost[%d]=%.18g want %.18g tolerance %.3g", index, value, want[index], tolerance)
							}
						}
					})
				}
			}
		}
	}
}

// Source: openai-responses-input-transformations.test.ts. All reports go
// through the registered providers, not a synthetic diagnostic emitter.
func TestSourceV160ResponsesInputTransformations(t *testing.T) {
	removal := func(id, reason string) map[string]any {
		return map[string]any{"type": "reasoning", "id": id, "action": "removed", "reason": reason}
	}
	prefix := removal("rs_edited", "prefix_mismatch")
	boundary := removal("rs_old_turn", "turn_boundary")
	organization := removal("rs_org", "organization_mismatch")
	future := removal("rs_future", "future_reason")
	cases := []struct {
		name, status, kind string
		report             any
		want               []any
		reason             goai.StopReason
	}{
		{"completed", "completed", "response.completed", []any{prefix, boundary, organization}, []any{prefix, organization}, goai.StopReasonStop},
		{"incomplete", "incomplete", "response.incomplete", []any{prefix}, []any{prefix}, goai.StopReasonLength},
		{"future", "completed", "response.completed", []any{future}, []any{future}, goai.StopReasonStop},
		{"codex", "completed", "response.done", []any{prefix}, []any{prefix}, goai.StopReasonStop},
	}
	for i, report := range []any{nil, nil, []any{}, []any{boundary, boundary}, map[string]any{"malformed": true}, []any{nil, "rs_text", map[string]any{"type": "message", "id": "msg_1", "action": "removed", "reason": "prefix_mismatch"}}, []any{map[string]any{"type": "reasoning", "id": "rs_changed", "action": "changed", "reason": "prefix_mismatch"}}} {
		cases = append(cases, struct {
			name, status, kind string
			report             any
			want               []any
			reason             goai.StopReason
		}{fmt.Sprintf("ignored-%d", i), "completed", "response.completed", report, nil, goai.StopReasonStop})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := map[string]any{"id": "r", "status": tc.status}
			if tc.name != "ignored-0" {
				response["input_transformations"] = tc.report
			}
			if tc.status == "incomplete" {
				response["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
			}
			frame, err := json.Marshal(map[string]any{"type": tc.kind, "response": response})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\n", frame)
			}))
			defer server.Close()
			api, provider, token := goai.ApiOpenAIResponses, goai.ProviderOpenAI, "test"
			if tc.name == "codex" {
				api, provider = goai.ApiOpenAICodexResponses, "openai-codex"
				token = "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct"}}`)) + ".x"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			model := &goai.Model{ID: "gpt-6.2", Api: api, Provider: provider, BaseURL: server.URL}
			var done *goai.DoneEvent
			for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("go")}}, &goai.StreamOptions{APIKey: token, Transport: goai.TransportSSE}) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done = e
				case *goai.ErrorEvent:
					t.Errorf("stream: %v", e.Err)
				}
			}
			if done == nil || done.Reason != tc.reason {
				t.Fatalf("terminal = %+v, want %q", done, tc.reason)
			}
			diagnostics := done.Message.Diagnostics
			if tc.want == nil {
				if len(diagnostics) != 0 {
					t.Fatalf("unexpected diagnostics: %+v", diagnostics)
				}
				return
			}
			if len(diagnostics) != 1 || diagnostics[0].Type != "openai_input_transformations" || diagnostics[0].Timestamp <= 0 || !reflect.DeepEqual(diagnostics[0].Error, goai.DiagnosticError{}) || !reflect.DeepEqual(diagnostics[0].Details, map[string]any{"transformations": tc.want}) {
				t.Fatalf("diagnostics = %+v, want %v", diagnostics, tc.want)
			}
			wire, err := json.Marshal(diagnostics[0])
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]json.RawMessage
			if err := json.Unmarshal(wire, &record); err != nil || len(record) != 3 || record["error"] != nil {
				t.Fatalf("diagnostic wire = %s, %v", wire, err)
			}
		})
	}
}
