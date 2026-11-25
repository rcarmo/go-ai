package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 anthropic-sse-parsing.test.ts:307–327. The pinned
// test sees beta options on an injected SDK client; Go observes the header
// that the native public adapter sends. Other rows are compatibility controls.
func TestSourceV160AnthropicInterleavedBetaRequiresThinking(t *testing.T) {
	goai.RegisterBuiltinModels()
	original := goai.GetModel(goai.ProviderOpenRouter, "anthropic/claude-haiku-4.5")
	if original == nil || original.Api != goai.ApiAnthropicMessages || !original.Reasoning {
		t.Fatalf("missing reasoning fixture model: %+v", original)
	}
	const interleaved = "interleaved-thinking-2025-05-14"
	off, low := goai.ThinkingLevel("off"), goai.ThinkingLow
	for _, tc := range []struct {
		name                             string
		reasoning                        *goai.ThinkingLevel
		nonReasoning, adaptive           bool
		modelBeta, requestBeta           string
		wantBeta, wantThinking, wantAuth string
	}{
		{name: "disabled", reasoning: &off, wantThinking: "disabled"},
		{name: "default-disabled", wantThinking: "disabled"},
		{name: "enabled-legacy", reasoning: &low, wantBeta: interleaved, wantThinking: "enabled"},
		{name: "nonreasoning", reasoning: &low, nonReasoning: true},
		{name: "enabled-adaptive", reasoning: &low, adaptive: true, wantThinking: "adaptive"},
		{name: "model-override", reasoning: &low, modelBeta: "fixture-model-beta", wantBeta: "fixture-model-beta", wantThinking: "enabled"},
		{name: "request-override", reasoning: &off, modelBeta: "fixture-model-beta", requestBeta: interleaved + ",fixture-request-beta",
			wantBeta: interleaved + ",fixture-request-beta", wantThinking: "disabled", wantAuth: "Bearer fixture-request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := *original
			model.Reasoning = !tc.nonReasoning
			if tc.adaptive {
				model.AnthropicCompat = &goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(true)}
			}
			if tc.modelBeta != "" {
				model.Headers = map[string]string{"anthropic-beta": tc.modelBeta}
			}
			opts := goai.StreamOptions{Reasoning: tc.reasoning, CacheRetention: goai.CacheRetentionNone}
			if tc.requestBeta != "" {
				opts.Headers = map[string]string{"anthropic-beta": tc.requestBeta, "authorization": tc.wantAuth}
			}
			ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}
			body, headers := captureSinglePublicAnthropicBetaRequestV160(t, &model, ctx, opts, "https://openrouter.ai/api/v1/messages", tc.wantAuth)
			var wantBeta []string
			if tc.wantBeta != "" {
				wantBeta = []string{tc.wantBeta}
			}
			if got := headers.Values("Anthropic-Beta"); !slices.Equal(got, wantBeta) {
				t.Errorf("Anthropic-Beta header = %q, want %q", got, wantBeta)
			}
			if tc.wantThinking == "" {
				if _, present := body["thinking"]; present {
					t.Errorf("thinking = %#v, want omitted", body["thinking"])
				}
			} else if thinking, _ := body["thinking"].(map[string]any); thinking["type"] != tc.wantThinking {
				t.Errorf("thinking = %#v, want type %q", thinking, tc.wantThinking)
			}
		})
	}
}

// Supplemental R54 cold-create comparison against the archived SDK capture.
// Only beta selection changes; query and historical plain-text form stay native.
func TestV160AnthropicColdThreadInterleavedBetaGate(t *testing.T) {
	const session = "thread-cold-beta-gate"
	if err := goai.CleanupSessionResources(session); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := goai.CleanupSessionResources(session); err != nil {
			t.Error(err)
		}
	})
	model := &goai.Model{ID: "claude-test", Name: "Test Claude", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
		BaseURL: anthropicThreadWireBaseV160, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 100}
	first, second := goai.UserMessage("first"), goai.UserMessage("second")
	first.Timestamp, second.Timestamp = 1, 1
	historical := goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID,
		ResponseID: "msg_history", Timestamp: 1, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "answer"}}}
	ctx := &goai.Context{Messages: []goai.Message{first, historical, second}, SystemPrompt: "You are a test assistant.",
		Tools: []goai.Tool{{Name: "lookup", Description: "Tool lookup", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}}
	var hook map[string]any
	hooks := 0
	opts := goai.StreamOptions{SessionID: session, OnPayload: func(value any, _ *goai.Model) (any, error) {
		hooks++
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return nil, json.Unmarshal(encoded, &hook)
	}}
	body, headers := captureSinglePublicAnthropicBetaRequestV160(t, model, ctx, opts, anthropicThreadWireBaseV160+"/v1/messages", "")
	if got := headers.Values("Anthropic-Beta"); !slices.Equal(got, []string{"message-threads-2026-08-12"}) {
		t.Errorf("cold thread beta = %q, want only the thread beta", got)
	}
	const messages = `[{"role":"user","content":[{"type":"text","text":"first"}]},{"role":"assistant","content":[{"type":"text","text":"answer"}]},{"role":"user","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}]}]`
	var want map[string]any
	if err := json.Unmarshal([]byte(anthropicThreadWireBodyV160(messages, `{"type":"create"}`, true)), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("cold thread body = %#v, want %#v", body, want)
	}
	delete(want, "thread")
	if hooks != 1 || !reflect.DeepEqual(hook, want) {
		t.Errorf("cold thread hook = %#v (%d calls), want %#v", hook, hooks, want)
	}
}

func captureSinglePublicAnthropicBetaRequestV160(t *testing.T, model *goai.Model, ctx *goai.Context, opts goai.StreamOptions, wantURL, wantAuth string) (map[string]any, http.Header) {
	t.Helper()
	zero := 0
	opts.APIKey, opts.MaxRetries = "test-key", &zero
	opts.Env = goai.ProviderEnv{"ANTHROPIC_AUTH_TOKEN": "", "PI_CACHE_RETENTION": ""}
	var body map[string]any
	var headers http.Header
	requests := 0
	opts.HTTPTransport = anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		requests++
		if requests != 1 || r.Method != http.MethodPost || r.URL.String() != wantURL || r.Header.Get("Anthropic-Version") != "2023-06-01" {
			return nil, fmt.Errorf("unexpected Anthropic fixture request %d: %s %s", requests, r.Method, r.URL)
		}
		var key, auth []string
		if wantAuth == "" {
			key = []string{"test-key"}
		} else {
			auth = []string{wantAuth}
		}
		if !slices.Equal(r.Header.Values("X-Api-Key"), key) || !slices.Equal(r.Header.Values("Authorization"), auth) {
			return nil, fmt.Errorf("unexpected synthetic Anthropic authentication")
		}
		headers = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(anthropicThreadReplyV160("msg_1", false)))}, nil
	})
	result, err := goai.Complete(t.Context(), model, ctx, &opts)
	if err != nil || result == nil || result.ResponseID != "msg_1" || result.StopReason != goai.StopReasonStop || goai.ContentText(result.Content) != "answer" || requests != 1 {
		t.Fatalf("result = %#v, err=%v requests=%d; want one request then msg_1/stop/answer", result, err, requests)
	}
	return body, headers
}
