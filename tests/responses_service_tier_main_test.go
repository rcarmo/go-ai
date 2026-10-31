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
