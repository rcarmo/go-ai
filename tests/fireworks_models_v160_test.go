package goai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/anthropic"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
)

// Source: pi v1.6.0 packages/ai/test/fireworks-models.test.ts:22,33,68.
func TestSourceV160FireworksModelProjection(t *testing.T) {
	t.Run("messages-route", func(t *testing.T) {
		model := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/deepseek-v4p1-flash")
		if model.Api != goai.ApiAnthropicMessages || model.Provider != goai.ProviderFireworks || model.BaseURL != "https://api.fireworks.ai/inference" || !model.Reasoning || !reflect.DeepEqual(model.Input, []string{"text", "image"}) {
			t.Fatalf("DeepSeek Flash route = %+v", model)
		}
	})
	t.Run("glm-fast", func(t *testing.T) {
		base := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/glm-5p3")
		fast := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/routers/glm-5p3-fast")
		if fast.Api != base.Api || fast.BaseURL != base.BaseURL || !reflect.DeepEqual(fast.CompletionsCompat, base.CompletionsCompat) || !reflect.DeepEqual(fast.ThinkingLevelMap, base.ThinkingLevelMap) {
			t.Fatalf("GLM Fast = %+v; want API, URL, compat and thinking levels of %+v", fast, base)
		}
	})
	t.Run("kimi-effort", func(t *testing.T) {
		base := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/kimi-k3")
		fast := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/routers/kimi-k3-fast")
		enabled, disabled := true, false
		compat := &goai.OpenAICompletionsCompat{
			SupportsStore:      &disabled,
			SupportsStrictMode: &enabled,
			RequiresReasoningContentOnAssistantMessages: &enabled,
			ThinkingFormat:                 "openai",
			SupportsMidConvoSystemMessages: &enabled,
			SupportsMidConvoToolAdditions:  &enabled,
			SendSessionAffinityHeaders:     &enabled,
			SupportsLongCacheRetention:     &disabled,
		}
		low, high, maximum := "low", "high", "max"
		thinking := map[goai.ModelThinkingLevel]*string{
			"off": nil, "minimal": nil, "low": &low, "medium": nil, "high": &high, "xhigh": nil, "max": &maximum,
		}
		for _, model := range []*goai.Model{base, fast} {
			if model.Api != goai.ApiOpenAICompletions || model.BaseURL != "https://api.fireworks.ai/inference/v1" {
				t.Fatalf("%s route = %s %s", model.ID, model.Api, model.BaseURL)
			}
			if !reflect.DeepEqual(model.CompletionsCompat, compat) {
				t.Fatalf("%s compat = %#v; want exact Fireworks Kimi compat %#v", model.ID, model.CompletionsCompat, compat)
			}
			if !reflect.DeepEqual(model.ThinkingLevelMap, thinking) {
				t.Fatalf("%s thinking levels = %#v; want %#v", model.ID, model.ThinkingLevelMap, thinking)
			}
		}

		// The original aborts in OnPayload. Use loopback for the safety net so
		// a missed hook can't send a request to the real provider.
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()
		model := *base
		model.BaseURL = server.URL
		requested := goai.ThinkingMax
		var captured map[string]json.RawMessage
		var calls int
		for range goai.GetApiProvider(model.Api).StreamSimple(t.Context(), &model, &goai.Context{Messages: []goai.Message{goai.UserMessage("test")}}, &goai.StreamOptions{
			APIKey: "test-fireworks-key", Reasoning: &requested,
			OnPayload: func(payload any, _ *goai.Model) (any, error) {
				calls++
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Error(err)
					return nil, err
				}
				if err = json.Unmarshal(encoded, &captured); err != nil {
					t.Error(err)
					return nil, err
				}
				return nil, errors.New("payload captured")
			},
		}) {
		}
		if calls != 1 || requests.Load() != 0 || string(captured["reasoning_effort"]) != `"max"` {
			t.Fatalf("payload hooks=%d requests=%d reasoning_effort=%s; want 1, 0, and max", calls, requests.Load(), captured["reasoning_effort"])
		}
	})
}

// Source: pi v1.6.0 packages/ai/test/fireworks-models.test.ts:241-324,335-429.
func TestSourceV160FireworksMessagesWire(t *testing.T) {
	no, yes := false, true
	anthropic := goai.Model{
		ID: "claude-opus-4-8", Name: "Claude Opus 4.8", Api: goai.ApiAnthropicMessages,
		Provider: goai.ProviderAnthropic, Reasoning: true, Input: []string{"text"},
		ContextWindow: 200000, MaxTokens: 32000,
	}
	fireworks := anthropic
	fireworks.ID, fireworks.Name, fireworks.Provider = "accounts/fireworks/models/kimi-k2p6", "Kimi K2.6", goai.ProviderFireworks
	fireworks.Input = []string{"text", "image"}
	fireworks.Cost = goai.ModelCost{Input: 0.95, Output: 4, CacheRead: 0.16}
	fireworks.ContextWindow, fireworks.MaxTokens = 262000, 262000
	fireworks.AnthropicCompat = &goai.AnthropicMessagesCompat{
		AllowEmptySignature: &yes, SendSessionAffinityHeaders: &yes,
		SupportsEagerToolInputStreaming: &no, SupportsCacheControlOnTools: &no, SupportsLongCacheRetention: &no,
	}
	openrouter := anthropic
	openrouter.ID, openrouter.Provider = "anthropic/claude-opus-4.8", goai.ProviderOpenRouter
	disabled := openrouter
	disabled.AnthropicCompat = &goai.AnthropicMessagesCompat{SendSessionAffinityHeaders: &no}
	withModelHeader := fireworks
	withModelHeader.Headers = map[string]string{"X-SESSION-AFFINITY": "model-session"}
	for _, tc := range []struct {
		name, session, wantAffinity, wantID string
		model                               goai.Model
		retention                           goai.CacheRetention
		headers                             map[string]string
		suppress                            []string
	}{
		{name: "fireworks-affinity", model: fireworks, session: "fireworks-session-1", wantAffinity: "fireworks-session-1"},   // 336
		{name: "anthropic-no-affinity", model: anthropic, session: "anthropic-session-1"},                                     // 346
		{name: "fireworks-no-cache", model: fireworks, session: "fireworks-session-2", retention: goai.CacheRetentionNone},    // 355
		{name: "openrouter-session-id", model: openrouter, session: "openrouter-session-1", wantID: "openrouter-session-1"},   // 366
		{name: "openrouter-no-cache", model: openrouter, session: "openrouter-session-2", retention: goai.CacheRetentionNone}, // 375
		{name: "openrouter-disabled", model: disabled, session: "openrouter-session-3"},                                       // 385
		// Supplemental controls for the header merge and empty session gates.
		{name: "empty-session", model: fireworks},
		{name: "model-affinity-override", model: withModelHeader, session: "derived", wantAffinity: "model-session"},
		{name: "caller-affinity-override", model: withModelHeader, session: "derived", headers: map[string]string{"x-session-affinity": "caller-session"}, wantAffinity: "caller-session"},
		{name: "caller-affinity-suppress", model: fireworks, session: "derived", suppress: []string{"X-Session-Affinity"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := goai.StreamOptions{SessionID: tc.session, CacheRetention: tc.retention, Headers: tc.headers, SuppressHeaders: tc.suppress}
			request := captureFireworksMessagesWire(t, tc.model, opts)
			for header, want := range map[string]string{"X-Session-Affinity": tc.wantAffinity, "X-Session-Id": tc.wantID} {
				got := request.headers.Values(header)
				if want == "" && got != nil || want != "" && (len(got) != 1 || got[0] != want) {
					t.Errorf("%s = %q; want %q (empty means absent)", header, got, want)
				}
			}
		})
	}
	for _, tc := range []struct {
		name      string
		model     goai.Model
		field     string
		want      string
		checkEach bool
	}{
		{"fireworks-no-tool-cache-control", fireworks, "cache_control", "", false},         // 394
		{"fireworks-no-eager-stream", fireworks, "eager_input_streaming", "", true},        // 403
		{"anthropic-tool-cache-control", anthropic, "cache_control", `"ephemeral"`, false}, // 413
		{"anthropic-eager-stream", anthropic, "eager_input_streaming", "true", true},       // 423 (only one tool)
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := captureFireworksMessagesWire(t, tc.model, goai.StreamOptions{})
			tools := request.tools
			if !tc.checkEach {
				tools = tools[len(tools)-1:]
			}
			for i, tool := range tools {
				v, present := tool[tc.field]
				if tc.want == "" {
					if present {
						t.Errorf("tool %d %s present as %s; want absent", i, tc.field, v)
					}
					continue
				}
				if tc.field == "cache_control" {
					var cache map[string]json.RawMessage
					if err := json.Unmarshal(v, &cache); err != nil {
						t.Fatalf("invalid tool cache_control: %s (%v)", v, err)
					}
					v = cache["type"]
				}
				if string(v) != tc.want {
					t.Errorf("tool %d %s = %s; want %s", i, tc.field, v, tc.want)
				}
			}
		})
	}
}

type fireworksMessagesRequest struct {
	headers http.Header
	tools   []map[string]json.RawMessage
}

func captureFireworksMessagesWire(t *testing.T, model goai.Model, opts goai.StreamOptions) fireworksMessagesRequest {
	t.Helper()
	captured := make(chan fireworksMessagesRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected wire request %s %s", r.Method, r.URL.Path)
		}
		var body struct{ Tools []map[string]json.RawMessage }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode Messages request: %v", err)
		}
		select {
		case captured <- fireworksMessagesRequest{r.Header.Clone(), body.Tools}:
		default:
			t.Error("unexpected second Messages request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	model.BaseURL = server.URL
	opts.APIKey = "test-key"
	opts.HTTPTransport = server.Client().Transport
	if opts.CacheRetention == "" {
		opts.CacheRetention = goai.CacheRetentionShort // Set explicitly, independent of ambient env.
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var lastError error
	for event := range goai.Stream(ctx, &model, &goai.Context{
		Messages: []goai.Message{goai.UserMessage("Use the tool")},
		Tools:    []goai.Tool{{Name: "lookup", Description: "Look up a value", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}},
	}, &opts) {
		if failure, ok := event.(*goai.ErrorEvent); ok {
			lastError = failure.Err
		}
	}
	select {
	case req := <-captured:
		if len(req.tools) == 0 {
			t.Fatal("expected tools in request body")
		}
		return req
	default:
		t.Fatalf("no loopback request captured (stream error: %v)", lastError)
		return fireworksMessagesRequest{}
	}
}
