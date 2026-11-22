package openaicodex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
)

func TestBuildCodexRequestClampsPromptCacheKey(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses}
	req := buildCodexRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{SessionID: strings.Repeat("🚀", 80)})
	if got, want := req.PromptCacheKey, strings.Repeat("🚀", 64); got != want {
		t.Fatalf("prompt cache key = %q, want %q", got, want)
	}
}

func TestBuildCodexRequestMatchesPiaiShape(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, Reasoning: true}
	sess := "sess-123"
	reasoning := goai.ThinkingLevel("minimal")
	conv := &goai.Context{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hi"}}}},
		Tools:        []goai.Tool{{Name: "shell", Description: "run shell", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	req := buildCodexRequest(model, conv, &goai.StreamOptions{SessionID: sess, Reasoning: &reasoning, ReasoningSummary: "detailed", TextVerbosity: "medium"})
	if !req.Stream {
		t.Fatalf("expected stream=true")
	}
	if req.Store {
		t.Fatalf("expected store to be false")
	}
	var input []map[string]any
	if err := json.Unmarshal(req.Input, &input); err != nil || len(input) < 1 || !reflect.DeepEqual(input[0], map[string]any{"role": "developer", "content": conv.SystemPrompt}) {
		t.Fatalf("expected leading instructions %q, got %s, %v", conv.SystemPrompt, req.Input, err)
	}
	if req.PromptCacheKey != sess {
		t.Fatalf("expected prompt cache key %q, got %q", sess, req.PromptCacheKey)
	}
	if req.ToolChoice != "auto" {
		t.Fatalf("expected tool_choice auto, got %q", req.ToolChoice)
	}
	if req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("expected parallel_tool_calls=true")
	}
	if len(req.Include) != 1 || req.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("unexpected include: %#v", req.Include)
	}
	rm, ok := req.Reasoning.(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning map, got %#v", req.Reasoning)
	}
	expectedEffort, ok := goai.MapThinkingLevel(model, goai.ModelThinkingLevel(reasoning))
	if !ok {
		t.Fatal("expected reasoning effort mapping")
	}
	if rm["effort"] != expectedEffort {
		t.Fatalf("expected mapped effort %q, got %#v", expectedEffort, rm["effort"])
	}
	if rm["summary"] != "detailed" {
		t.Fatalf("expected reasoning summary override, got %#v", rm["summary"])
	}
	text, ok := req.Text.(map[string]interface{})
	if !ok || text["verbosity"] != "medium" {
		t.Fatalf("expected text verbosity override, got %#v", req.Text)
	}
	if len(input) < 2 || input[1]["role"] != "user" {
		t.Fatalf("expected user message after instructions, got %#v", input)
	}
}

func TestStreamCodexMissingAPIKeyErrorMatchesUpstream(t *testing.T) {
	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: "https://example.invalid/v1"}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	for ev := range streamCodex(context.Background(), model, convCtx, nil) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			if e.Err == nil || e.Err.Error() != "No API key for provider: openai-codex" {
				t.Fatalf("unexpected missing-key error: %v", e.Err)
			}
			return
		}
	}
	t.Fatal("expected error event")
}

func TestExtractCodexEventErrorUsesNestedPayload(t *testing.T) {
	eventErr := extractCodexEventError("", "", &codexEventError{Code: "websocket_connection_limit_reached", Message: "too many connections"})
	if eventErr.Code != "websocket_connection_limit_reached" || eventErr.Message != "too many connections" {
		t.Fatalf("unexpected nested error: %#v", eventErr)
	}
	if !isWebSocketConnectionLimitReachedError(&codexAPIError{code: eventErr.Code}) {
		t.Fatal("expected connection limit error to be detected")
	}
}

func TestBuildCodexHeadersAddsAccountAndExperimentalHeaders(t *testing.T) {
	token := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	accountID, err := extractCodexAccountID(token)
	if err != nil {
		t.Fatalf("extractCodexAccountID: %v", err)
	}
	if accountID != "acct_123" {
		t.Fatalf("unexpected account id: %q", accountID)
	}
	h := buildCodexSSEHeaders(map[string]string{"x-model": "1", "x-remove": "model"}, map[string]string{"x-opt": "2", "x-remove": "opt"}, []string{"x-remove"}, accountID, token, "sess-1")
	if h.Get("chatgpt-account-id") != "acct_123" || h.Get("OpenAI-Beta") != "responses=experimental" || h.Get("session-id") != "sess-1" || h.Get("session_id") != "" || h.Get("x-remove") != "" {
		t.Fatalf("unexpected SSE headers: %#v", h)
	}
	wh := buildCodexWebSocketHeaders(nil, nil, nil, accountID, token, "req-1")
	if wh.Get("OpenAI-Beta") != "responses_websockets=2026-02-06" || wh.Get("x-client-request-id") != "req-1" || wh.Get("session-id") != "req-1" || wh.Get("session_id") != "" {
		t.Fatalf("unexpected WS headers: %#v", wh)
	}
}

// Source: openai-codex-stream.test.ts, request projection. Capture the SSE
// request and inspect the returned assistant's provider effort marker.
func TestSourceV160CodexRequestProjection(t *testing.T) {
	for _, tc := range []struct {
		name, system, level       string
		previous                  bool
		cap, wantCap              int
		supportEffort, capAllowed bool
		wantEffort, wantMarker    string
	}{
		{"first", "Be terse.", "low", false, 5000, 5000, true, true, "medium", "low"},
		{"later", "Be terse.", "high", true, 5000, 5000, true, true, "medium", "high"},
		{"off", "", "", false, 2, 16, true, true, "medium", "none"},
		{"plain", "", "high", false, 2, 0, false, false, "high", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, captured := captureCodexSSEProjection(t, `{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`)
			model := &goai.Model{ID: "gpt-6.2", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, Reasoning: true, BaseURL: url, ContextWindow: 100000, MaxTokens: 10000,
				ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsMidConvoEffort: &tc.supportEffort, SupportsMaxOutputTokens: &tc.capAllowed},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			normal := &goai.Context{SystemPrompt: tc.system, Messages: []goai.Message{goai.UserMessage("first")}}
			if tc.previous {
				normal.Messages = append(normal.Messages, goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, ProviderThinkingLevel: "low", Content: []goai.ContentBlock{{Type: "text", Text: "answer"}}}, goai.UserMessage("second"))
			}
			token := "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct"}}`)) + ".x"
			opts := &goai.StreamOptions{APIKey: token, Transport: goai.TransportSSE, MaxTokens: &tc.cap}
			if tc.level != "" {
				level := goai.ThinkingLevel(tc.level)
				opts.Reasoning = &level
			}
			var done *goai.Message
			for ev := range streamCodexSimple(ctx, model, normal, opts) {
				switch e := ev.(type) {
				case *goai.DoneEvent:
					done = e.Message
				case *goai.ErrorEvent:
					t.Errorf("stream error: %v", e.Err)
				}
			}
			if done == nil {
				t.Fatal("missing result")
			}
			if done.ProviderThinkingLevel != tc.wantMarker {
				t.Errorf("providerThinkingLevel = %q, want %q", done.ProviderThinkingLevel, tc.wantMarker)
			}
			var body map[string]any
			select {
			case body = <-captured:
			case <-ctx.Done():
				t.Fatal("missing request")
			}
			var wantCap any
			if tc.wantCap != 0 {
				wantCap = float64(tc.wantCap)
			}
			if body["instructions"] != nil || body["max_output_tokens"] != wantCap || !reflect.DeepEqual(body["reasoning"], map[string]any{"effort": tc.wantEffort, "summary": "auto"}) {
				t.Errorf("request controls = %v, want no instructions, cap %v and effort %s", body, wantCap, tc.wantEffort)
			}
			var order []string
			for _, raw := range body["input"].([]any) {
				item := raw.(map[string]any)
				switch item["type"] {
				case "configuration_update":
					order = append(order, "configuration_update:"+item["reasoning"].(map[string]any)["effort"].(string))
				default:
					order = append(order, fmt.Sprint(item["role"]))
				}
			}
			var want []string
			if tc.system != "" {
				want = append(want, "developer")
			}
			want = append(want, "user")
			if tc.previous {
				want = append(want, "configuration_update:low", "assistant", "user")
			}
			if tc.wantMarker != "" {
				want = append(want, "configuration_update:"+tc.wantMarker)
			}
			if !reflect.DeepEqual(order, want) {
				t.Errorf("input order=%v, want %v", order, want)
			}
		})
	}
}

func captureCodexSSEProjection(t *testing.T, frames ...string) (string, <-chan map[string]any) {
	t.Helper()
	bodies := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "zstd" {
			decoder, err := zstd.NewReader(reader)
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
		select {
		case bodies <- body:
		default:
			t.Error("unexpected extra SSE request")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range frames {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, bodies
}

// Exact pinned alternatives to the separate request-property matrix above.
func TestSourceV160CodexRequestProjectionExact(t *testing.T) {
	for _, mode := range []string{"developer-prompt", "request-effort", "output-token-cap"} {
		t.Run(mode, func(t *testing.T) {
			url, bodies := captureCodexSSEProjection(t,
				`{"type":"response.output_item.added","item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
				`{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`,
				`{"type":"response.output_text.delta","delta":"Hello"}`,
				`{"type":"response.output_item.done","item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello"}]}}`,
				`{"type":"response.completed","response":{"status":"completed","incomplete_details":null,"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8,"input_tokens_details":{"cached_tokens":0}}}}`,
			)
			model := &goai.Model{ID: "gpt-6.2-sol", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, Reasoning: true, BaseURL: url, ContextWindow: 400_000, MaxTokens: 128_000}
			input := goai.Context{Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "Say hello"}}, Timestamp: 1}}}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			run := func(opts goai.StreamOptions, simple bool) (*goai.Message, map[string]any) {
				t.Helper()
				opts.APIKey, opts.Transport = payloadJWT, goai.TransportSSE
				stream := goai.Stream
				if simple {
					stream = goai.GetApiProvider(model.Api).StreamSimple
				}
				var result *goai.Message
				for ev := range stream(ctx, model, &input, &opts) {
					switch e := ev.(type) {
					case *goai.ErrorEvent:
						t.Fatalf("SSE request: %v", e.Err)
					case *goai.DoneEvent:
						result = e.Message
					}
				}
				if result == nil || result.StopReason != goai.StopReasonStop || goai.ContentText(result.Content) != "Hello" {
					t.Fatalf("SSE result = %+v; want successful Hello", result)
				}
				select {
				case body := <-bodies:
					return result, body
				case <-ctx.Done():
					t.Fatal("missing SSE body")
					return nil, nil
				}
			}
			switch mode {
			case "developer-prompt":
				input.SystemPrompt = "Be terse."
				_, first := run(goai.StreamOptions{}, false)
				input.SystemPrompt = ""
				_, second := run(goai.StreamOptions{}, false)
				user := map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Say hello"}}}
				expected := [][]any{{map[string]any{"role": "developer", "content": "Be terse."}, user}, {user}}
				for i, body := range []map[string]any{first, second} {
					if _, present := body["instructions"]; present {
						t.Errorf("request %d includes instructions = %v; want absent", i+1, body["instructions"])
					}
					if !reflect.DeepEqual(body["input"], expected[i]) {
						t.Errorf("request %d input = %v; want %v", i+1, body["input"], expected[i])
					}
				}
			case "request-effort":
				yes := true
				model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoEffort: &yes}
				low, high := goai.ThinkingLow, goai.ThinkingHigh
				first, firstBody := run(goai.StreamOptions{Reasoning: &low}, false)
				input.Messages = append(input.Messages, *first, goai.Message{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "Again"}}, Timestamp: 2})
				second, secondBody := run(goai.StreamOptions{Reasoning: &high}, false)
				if first.ProviderThinkingLevel != "low" || second.ProviderThinkingLevel != "high" {
					t.Errorf("response markers = %q, %q; want low then high", first.ProviderThinkingLevel, second.ProviderThinkingLevel)
				}
				want := [][]string{{"user", "configuration_update:low"}, {"user", "configuration_update:low", "assistant", "user", "configuration_update:high"}}
				for i, body := range []map[string]any{firstBody, secondBody} {
					if !reflect.DeepEqual(body["reasoning"], map[string]any{"effort": "medium", "summary": "auto"}) {
						t.Errorf("request %d reasoning = %v; want medium auto", i+1, body["reasoning"])
					}
					if got := summarizeCodexProjection(t, body["input"]); !reflect.DeepEqual(got, want[i]) {
						t.Errorf("request %d input order = %v; want %v", i+1, got, want[i])
					}
				}
			case "output-token-cap":
				cap, low := 5000, 4
				_, first := run(goai.StreamOptions{MaxTokens: &cap}, true)
				_, second := run(goai.StreamOptions{MaxTokens: &low}, false)
				no := false
				model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMaxOutputTokens: &no}
				_, third := run(goai.StreamOptions{MaxTokens: &cap}, false)
				if first["max_output_tokens"] != float64(5000) || second["max_output_tokens"] != float64(16) {
					t.Errorf("max_output_tokens = %v, %v; want 5000 and 16", first["max_output_tokens"], second["max_output_tokens"])
				}
				if _, present := third["max_output_tokens"]; present {
					t.Errorf("model opt-out includes max_output_tokens = %v; want absent", third["max_output_tokens"])
				}
			}
			if len(bodies) != 0 {
				t.Error("unexpected extra captured body")
			}
		})
	}
}

func summarizeCodexProjection(t *testing.T, input any) []string {
	t.Helper()
	items, ok := input.([]any)
	if !ok {
		t.Fatalf("request input = %v; want array", input)
	}
	order := make([]string, 0, len(items))
	for _, raw := range items {
		item := raw.(map[string]any)
		switch item["type"] {
		case "configuration_update":
			order = append(order, "configuration_update:"+fmt.Sprint(item["reasoning"].(map[string]any)["effort"]))
		default:
			if role, ok := item["role"].(string); ok {
				order = append(order, role)
			} else {
				order = append(order, fmt.Sprint(item["type"]))
			}
		}
	}
	return order
}

// max-thinking.test.ts captures real provider payloads before transport.
func TestSourceV160CodexMaxThinkingRequest(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, id := range []string{"gpt-5.6-sol", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-astra", "gpt-6.2-sol"} {
		t.Run(id, func(t *testing.T) {
			catalog := goai.GetModel(goai.ProviderOpenAICodex, id)
			if catalog == nil || catalog.Api != goai.ApiOpenAICodexResponses {
				t.Fatal("missing pinned Codex model")
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				http.Error(w, "payload crossed the transport boundary", http.StatusBadRequest)
			}))
			defer server.Close()
			model := *catalog
			model.BaseURL = server.URL
			stop := errors.New("payload captured")
			var payload map[string]json.RawMessage
			max := goai.ThinkingLevel(goai.ThinkingMax)
			captured, stopped := 0, 0
			opts := &goai.StreamOptions{APIKey: payloadJWT, Reasoning: &max, OnPayload: func(request any, _ *goai.Model) (any, error) {
				captured++
				wire, err := json.Marshal(request)
				if err != nil {
					return nil, err
				}
				if err := json.Unmarshal(wire, &payload); err != nil {
					return nil, err
				}
				return nil, stop
			}}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			input := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Hello")}}
			for event := range goai.Stream(ctx, &model, input, opts) {
				if err, ok := event.(*goai.ErrorEvent); ok && errors.Is(err.Err, stop) {
					stopped++
				} else {
					t.Errorf("unexpected payload capture event: %T %v", event, event)
				}
			}
			if captured != 1 || stopped != 1 || requests.Load() != 0 {
				t.Fatalf("payload captures = %d, stops = %d, network calls = %d; want 1, 1, 0", captured, stopped, requests.Load())
			}
			if id == "gpt-5.6-sol" {
				var reasoning map[string]any
				if err := json.Unmarshal(payload["reasoning"], &reasoning); err != nil || !reflect.DeepEqual(reasoning, map[string]any{"effort": "max", "summary": "auto"}) {
					t.Errorf("Codex reasoning = %s; want max effort and auto summary", payload["reasoning"])
				}
				return
			}
			var items []map[string]any
			want := map[string]any{"type": "configuration_update", "reasoning": map[string]any{"effort": "max"}}
			if err := json.Unmarshal(payload["input"], &items); err != nil || len(items) == 0 || !reflect.DeepEqual(items[len(items)-1], want) {
				t.Errorf("Codex request input = %s; want exact terminal max configuration_update", payload["input"])
			}
		})
	}
}
