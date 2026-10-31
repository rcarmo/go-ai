package mistral

// Ports of packages/ai/test/mistral-{reasoning-mode,raw-stop-reason,tool-schema}.test.ts
// at 4b535d57ea8863dbba44b89872146523da271786. Go's hook observes wire names;
// TypeBox symbol keys have no representation in JSON-backed Go tool schemas.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func upstreamMistralPayload(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	err = json.Unmarshal(data, &object)
	return object, err
}

func upstreamMistralModel(id string) *goai.Model {
	return &goai.Model{ID: id, Name: id, Api: goai.ApiMistralConversations, Provider: goai.ProviderMistral, BaseURL: "http://127.0.0.1:9", Input: []string{"text"}, ContextWindow: 128000, MaxTokens: 16384}
}

func upstreamMistralResult(t *testing.T, ctx context.Context, model *goai.Model, transcript *goai.Context, opts *goai.StreamOptions) *goai.Message {
	t.Helper()
	var result *goai.Message
	for event := range streamMistral(ctx, model, transcript, opts) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			result = e.Message
		case *goai.ErrorEvent:
			result = e.Error
			if result == nil {
				result = &goai.Message{StopReason: e.Reason}
				if e.Err != nil {
					result.ErrorMessage = e.Err.Error()
				}
			}
		}
	}
	if result == nil {
		t.Fatal("missing terminal result")
	}
	return result
}

func TestUpstreamMainMistralReasoningMode(t *testing.T) {
	noneHigh := func() map[goai.ModelThinkingLevel]*string {
		return map[goai.ModelThinkingLevel]*string{"off": strPtr("none"), "minimal": nil, "low": nil, "medium": nil, "high": strPtr("high"), "xhigh": nil, "max": nil}
	}
	type testCase struct {
		name, id                       string
		reasoning                      bool
		mapping                        map[goai.ModelThinkingLevel]*string
		level, effort, prompt, session string
		retention                      goai.CacheRetention
	}
	cases := []testCase{{name: "uses prompt_mode for reasoning models without a map", id: "magistral-medium-latest", reasoning: true, level: "medium", prompt: "reasoning"}, {name: "omits Magistral controls when thinking is off", id: "magistral-medium-latest", reasoning: true}}
	for _, id := range []string{"mistral-small-2603", "mistral-medium-latest", "zai-glm-5-2"} {
		mapping := noneHigh()
		if id == "zai-glm-5-2" {
			mapping["max"] = strPtr("max")
		}
		for _, c := range []struct{ name, level, effort string }{{"uses effort when enabled", "high", "high"}, {"clamps unsupported low", "low", "high"}, {"sends none when off", "", "none"}} {
			cases = append(cases, testCase{name: id + "/" + c.name, id: id, reasoning: true, mapping: mapping, level: c.level, effort: c.effort})
		}
	}
	glm52 := noneHigh()
	glm52["max"] = strPtr("max")
	cases = append(cases, testCase{name: "sends max for GLM-5.2", id: "zai-glm-5-2", reasoning: true, mapping: glm52, level: "max", effort: "max"})
	glm53 := noneHigh()
	glm53["off"] = nil
	glm53["low"] = strPtr("low")
	glm53["max"] = strPtr("max")
	for _, level := range []string{"low", "high", "max", "medium"} {
		effort := level
		if level == "medium" {
			effort = "high"
		}
		cases = append(cases, testCase{name: "GLM-5.3/" + level, id: "zai-glm-5-3", reasoning: true, mapping: glm53, level: level, effort: effort})
	}
	cases = append(cases, testCase{name: "omits controls for non-reasoning models", id: "mistral-medium-2505", level: "medium"}, testCase{name: "uses session id as cache key", id: "mistral-large-latest", session: "session-123"}, testCase{name: "omits cache key when retention disabled", id: "mistral-large-latest", session: "session-123", retention: goai.CacheRetentionNone})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := upstreamMistralModel(tc.id)
			model.Reasoning = tc.reasoning
			model.ThinkingLevelMap = tc.mapping
			var captured map[string]any
			sentinel := errors.New("payload captured before request")
			opts := &goai.StreamOptions{APIKey: "fake-key", SessionID: tc.session, CacheRetention: tc.retention, OnPayload: func(value any, _ *goai.Model) (any, error) {
				var err error
				captured, err = upstreamMistralPayload(value)
				if err != nil {
					return nil, err
				}
				return nil, sentinel
			}}
			if tc.level != "" {
				level := goai.ThinkingLevel(tc.level)
				opts.Reasoning = &level
			}
			upstreamMistralResult(t, context.Background(), model, makeMistralContext(), opts)
			if captured == nil {
				t.Fatal("payload not captured")
			}
			for key, want := range map[string]string{"reasoning_effort": tc.effort, "prompt_mode": tc.prompt, "prompt_cache_key": func() string {
				if tc.retention == goai.CacheRetentionNone {
					return ""
				}
				return tc.session
			}()} {
				got, present := captured[key]
				if want == "" {
					if present {
						t.Errorf("unexpected %s=%v", key, got)
					}
				} else if got != want {
					t.Errorf("%s=%v want %s", key, got, want)
				}
			}
		})
	}
}

func TestUpstreamMainMistralRawStopReasons(t *testing.T) {
	for _, tc := range []struct {
		finish  string
		reason  goai.StopReason
		message string
		retry   bool
	}{{"stop", goai.StopReasonStop, "", false}, {"error", goai.StopReasonError, "Provider stopped with: error (server error)", true}, {"unmapped_error", goai.StopReasonError, "Provider stopped with: unmapped_error", false}} {
		t.Run(tc.finish, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"id\":\"mistral-response-id\",\"model\":\"devstral-medium-latest\",\"choices\":[{\"index\":0,\"finish_reason\":%q,\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\ndata: [DONE]\n\n", tc.finish)
			}))
			defer server.Close()
			model := upstreamMistralModel("devstral-medium-latest")
			model.BaseURL = server.URL
			message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test"})
			if message.StopReason != tc.reason || message.RawStopReason != tc.finish || message.ErrorMessage != tc.message {
				t.Fatalf("message %#v", message)
			}
			if goai.IsRetryableAssistantError(message) != tc.retry {
				t.Fatalf("retryable=%v want %v", goai.IsRetryableAssistantError(message), tc.retry)
			}
		})
	}
}

func TestUpstreamMainMistralToolSchema(t *testing.T) {
	parameters := json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}},"required":["nested"]}`)
	transcript := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hi")}, Tools: []goai.Tool{{Name: "inspect_schema", Description: "Inspect the schema", Parameters: parameters, ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "require"}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	model := upstreamMistralModel("devstral-medium-latest")
	model.BaseURL = server.URL
	var captured map[string]any
	response := upstreamMistralResult(t, context.Background(), model, transcript, &goai.StreamOptions{APIKey: "fake-key", OnPayload: func(value any, _ *goai.Model) (any, error) {
		var err error
		captured, err = upstreamMistralPayload(value)
		return value, err
	}})
	if captured == nil {
		t.Fatal("payload not captured")
	}
	tools, ok := captured["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatal("tools", captured["tools"])
	}
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if function["strict"] != true {
		t.Fatal("strict not enabled", function)
	}
	schema := function["parameters"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	nested := properties["nested"].(map[string]any)
	if nested["properties"].(map[string]any)["value"].(map[string]any)["type"] != "string" {
		t.Fatal("nested schema lost", schema)
	}
	if response.StopReason != goai.StopReasonError || strings.Contains(response.ErrorMessage, "Input validation failed") {
		t.Fatalf("response %#v", response)
	}
}
