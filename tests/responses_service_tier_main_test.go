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
	"strings"
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

// Source: pi v1.6.0 openai-responses-input-transformations.test.ts:74–172.
// Exercise the shared terminal fixture and Codex SSE fixture through the public
// Go stream, not a synthetic diagnostic emitter.
func TestSourceV160ResponsesInputTransformations(t *testing.T) {
	removal := func(id, reason string) map[string]any {
		return map[string]any{"type": "reasoning", "id": id, "action": "removed", "reason": reason}
	}
	prefix := removal("rs_edited", "prefix_mismatch")
	boundary := removal("rs_old_turn", "turn_boundary")
	organization := removal("rs_org", "organization_mismatch")
	future := removal("rs_future", "future_reason")
	type transformationCase struct {
		name, status, kind string
		report             any
		want               []any
		reason             goai.StopReason
	}
	cases := []transformationCase{
		{"completed", "completed", "response.completed", []any{prefix, boundary, organization}, []any{prefix, organization}, goai.StopReasonStop},
		{"incomplete", "incomplete", "response.incomplete", []any{prefix}, []any{prefix}, goai.StopReasonLength},
		{"future", "completed", "response.completed", []any{future}, []any{future}, goai.StopReasonStop},
		{"codex", "completed", "response.done", []any{prefix, boundary}, []any{prefix}, goai.StopReasonStop},
	}
	for i, report := range []any{
		nil, // The field is omitted in the first row and explicitly null in the second.
		nil,
		[]any{},
		[]any{boundary, boundary},
		map[string]any{"malformed": true},
		[]any{nil, "rs_text", map[string]any{"type": "message", "id": "msg_1", "action": "removed", "reason": "prefix_mismatch"}},
		[]any{map[string]any{"type": "reasoning", "id": "rs_changed", "action": "changed", "reason": "prefix_mismatch"}},
	} {
		cases = append(cases, transformationCase{fmt.Sprintf("ignored-%d", i), "completed", "response.completed", report, nil, goai.StopReasonStop})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, provider := goai.ApiOpenAIResponses, goai.ProviderOpenAI
			base, path, responseID, token, account := "https://api.openai.com/v1", "/responses", "resp_transformations", "test-key", ""
			if tc.name == "codex" {
				api, provider = goai.ApiOpenAICodexResponses, goai.ProviderOpenAICodex
				base, path, responseID, account = "https://chatgpt.com/backend-api", "/codex/responses", "resp_codex", "acc_test"
				token = "aaa." + base64.URLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acc_test"}}`)) + ".bbb"
			}
			response := map[string]any{"id": responseID, "status": tc.status, "output": []any{}}
			if tc.name != "ignored-0" {
				response["input_transformations"] = tc.report
			}
			if tc.status == "incomplete" {
				response["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
			}
			frame := map[string]any{"type": tc.kind, "response": response}
			if tc.name != "codex" {
				frame["sequence_number"] = 0
			}
			wire, err := json.Marshal(frame)
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			transport := providerRetryTransportMain(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if r.Method != http.MethodPost || r.URL.String() != base+path || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("ChatGPT-Account-Id") != account {
					return nil, fmt.Errorf("unexpected fixture request %s %s or synthetic auth", r.Method, r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader("data: " + string(wire) + "\n\n"))}, nil
			})
			zero := 0
			model := &goai.Model{ID: "gpt-6.2-sol", Name: "GPT-6.2 Sol", Api: api, Provider: provider, BaseURL: base,
				Reasoning: true, Input: []string{"text"}, ContextWindow: 272000, MaxTokens: 128000}
			user := goai.UserMessage("Hello")
			user.Timestamp = 1
			var done *goai.DoneEvent
			for event := range goai.Stream(t.Context(), model, &goai.Context{Messages: []goai.Message{user}}, &goai.StreamOptions{
				APIKey: token, Transport: goai.TransportSSE, HTTPTransport: transport, MaxRetries: &zero,
			}) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					if done != nil {
						t.Error("multiple terminal events")
					}
					done = e
				case *goai.ErrorEvent:
					t.Errorf("stream: %v", e.Err)
				}
			}
			if requests != 1 {
				t.Errorf("sent %d requests, want exactly one", requests)
			}
			if done == nil || done.Message == nil || done.Reason != tc.reason || done.Message.StopReason != tc.reason || done.Message.ResponseID != responseID {
				t.Fatalf("terminal = %+v, want %q with message %q", done, tc.reason, responseID)
			}
			diagnostics := done.Message.Diagnostics
			if tc.want == nil {
				if diagnostics != nil {
					t.Fatalf("unexpected diagnostics: %+v", diagnostics)
				}
				encoded, err := json.Marshal(done.Message)
				if err != nil {
					t.Fatal(err)
				}
				var message map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &message); err != nil || message["diagnostics"] != nil {
					t.Fatalf("assistant message diagnostics must be omitted: %s, %v", encoded, err)
				}
				return
			}
			if len(diagnostics) != 1 || diagnostics[0].Type != "openai_input_transformations" || diagnostics[0].Timestamp <= 0 || !reflect.DeepEqual(diagnostics[0].Error, goai.DiagnosticError{}) || !reflect.DeepEqual(diagnostics[0].Details, map[string]any{"transformations": tc.want}) {
				t.Fatalf("diagnostics = %+v, want %v", diagnostics, tc.want)
			}
			encoded, err := json.Marshal(diagnostics[0])
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &record); err != nil || len(record) != 3 || record["error"] != nil {
				t.Fatalf("diagnostic wire = %s, %v", encoded, err)
			}
		})
	}
}
