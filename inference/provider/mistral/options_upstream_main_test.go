package mistral

// Ports of packages/ai/test/mistral-{reasoning-mode,raw-stop-reason,tool-schema}.test.ts
// at 4b535d57ea8863dbba44b89872146523da271786. Go's hook observes wire names;
// TypeBox symbol keys have no representation in JSON-backed Go tool schemas.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
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

// Source: pi v1.6.1 mistral-raw-stop-reason.test.ts. Exercise the public
// dispatch with the pinned model and complete synthetic provider responses.
func TestUpstreamMainMistralRawStopReasons(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderMistral, "mistral-medium-latest")
	if model == nil || model.Api != goai.ApiMistralConversations {
		t.Fatalf("unexpected pinned Mistral model: %+v", model)
	}
	for _, tc := range []struct {
		finish  string
		reason  goai.StopReason
		message string
		retry   bool
	}{{"stop", goai.StopReasonStop, "", false}, {"error", goai.StopReasonError, "Provider stopped with: error", true}, {"unmapped_error", goai.StopReasonError, "Provider stopped with: unmapped_error", false}} {
		t.Run(tc.finish, func(t *testing.T) {
			wire := fmt.Sprintf("data: {\"id\":\"mistral-response-id\",\"model\":%q,\"choices\":[{\"index\":0,\"finish_reason\":%q,\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\ndata: [DONE]\n\n", model.ID, tc.finish)
			requests := 0
			var requestURL string
			transport := mistralSchemaTransportV160(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				requestURL = r.URL.String()
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test" {
					return nil, fmt.Errorf("unexpected fixture request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			})
			zero := 0
			transcript := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
			var message *goai.Message
			for event := range goai.Stream(t.Context(), model, transcript, &goai.StreamOptions{APIKey: "test", MaxRetries: &zero, HTTPTransport: transport}) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					message = e.Message
				case *goai.ErrorEvent:
					message = e.Error
				}
			}
			if requests != 1 || message == nil {
				t.Fatalf("expected one request and a terminal message, got %d requests and %#v", requests, message)
			}
			if requestURL != "https://api.mistral.ai/v1/chat/completions" {
				t.Errorf("unexpected Mistral catalog endpoint: %q", requestURL)
			}
			if message.StopReason != tc.reason {
				t.Errorf("stop reason = %q; want %q", message.StopReason, tc.reason)
			}
			if message.RawStopReason != tc.finish {
				t.Errorf("raw stop reason = %q; want %q", message.RawStopReason, tc.finish)
			}
			if message.ErrorMessage != tc.message {
				t.Errorf("diagnostic = %q; want %q", message.ErrorMessage, tc.message)
			}
			if got := goai.IsRetryableAssistantError(message); got != tc.retry {
				t.Errorf("retryable = %v; want %v", got, tc.retry)
			}
		})
	}
}

// Source: pi v1.6.1 mistral-tool-schema.test.ts:18–63. TypeBox's symbol
// keys are tested only in JS; this test observes the exact JSON projection.
func TestUpstreamMainMistralToolSchema(t *testing.T) {
	testMistralToolSchemaWireV160(t, "mistral-medium-latest", true, goai.Tool{
		Name: "inspect_schema", Description: "Inspect the schema",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}},"required":["nested"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "require"},
	}, `[{
		"type":"function","function":{
			"name":"inspect_schema","description":"Inspect the schema","strict":true,
			"parameters":{"type":"object","required":["nested"],"additionalProperties":false,
				"properties":{"nested":{"type":"object","required":["value"],"additionalProperties":false,
					"properties":{"value":{"type":"string"}}}}}
		}
	}]`)
}

// Source: pi v1.6.0 mistral-tool-schema.test.ts:66–94. Large 4 leaves limit
// optional and sends an explicit false strict bit with its default compatibility.
func TestSourceV160MistralStrictModeIsOptIn(t *testing.T) {
	testMistralToolSchemaWireV160(t, "mistral-large-4", false, goai.Tool{
		Name: "read", Description: "Read a file",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"limit":{"type":"number"}},"required":["path"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"},
	}, `[{
		"type":"function","function":{
			"name":"read","description":"Read a file","strict":false,
			"parameters":{"type":"object","required":["path"],
				"properties":{"path":{"type":"string"},"limit":{"type":"number"}}}
		}
	}]`)
}

type mistralSchemaTransportV160 func(*http.Request) (*http.Response, error)

func (f mistralSchemaTransportV160) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testMistralToolSchemaWireV160(t *testing.T, modelID string, enableStrict bool, tool goai.Tool, wantJSON string) {
	t.Helper()
	// The native model registry starts empty until its built-ins are registered.
	goai.RegisterBuiltinModels()
	catalogModel := goai.GetModel(goai.ProviderMistral, modelID)
	if catalogModel == nil || catalogModel.Api != goai.ApiMistralConversations {
		t.Fatalf("unexpected Mistral catalog model %q: %+v", modelID, catalogModel)
	}
	model := *catalogModel
	// JS appends v1/chat/completions to the base; Go accepts a versioned base.
	model.BaseURL = "http://127.0.0.1:9/v1"
	if enableStrict {
		model.MistralCompat = &goai.MistralConversationsCompat{SupportsStrictMode: true}
	}
	var want []any
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	const responseBody = `{"message":"scripted Mistral rejection"}`
	var order []string
	var hook, wire map[string]any
	transport := mistralSchemaTransportV160(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		order = append(order, "transport")
		if r.Method != http.MethodPost || r.URL.String() != "http://127.0.0.1:9/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fake-key" {
			return nil, fmt.Errorf("unexpected Mistral fixture request %s %s or synthetic auth", r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(responseBody))}, nil
	})
	zero := 0
	transcript := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hi")}, Tools: []goai.Tool{tool}}
	var terminal *goai.ErrorEvent
	events := 0
	for event := range goai.Stream(t.Context(), &model, transcript, &goai.StreamOptions{
		APIKey: "fake-key", MaxRetries: &zero, HTTPTransport: transport,
		OnPayload: func(value any, _ *goai.Model) (any, error) {
			order = append(order, "hook")
			var err error
			hook, err = upstreamMistralPayload(value)
			return value, err
		},
	}) {
		events++
		terminal, _ = event.(*goai.ErrorEvent)
	}
	if !slices.Equal(order, []string{"hook", "transport"}) {
		t.Errorf("payload and transport order = %v; want a single hook before one request", order)
	}
	if !reflect.DeepEqual(hook["tools"], want) {
		t.Errorf("hook tools = %#v; want %s", hook["tools"], wantJSON)
	}
	if !reflect.DeepEqual(wire["tools"], want) {
		t.Errorf("wire tools = %#v; want %s", wire["tools"], wantJSON)
	}
	if events != 1 || terminal == nil || terminal.Reason != goai.StopReasonError || terminal.Err == nil ||
		terminal.Err.Error() != "Mistral API error (403): "+responseBody || strings.Contains(terminal.Err.Error(), "Input validation failed") {
		t.Fatalf("terminal = %+v (%d events), want one scripted 403 error after transport", terminal, events)
	}
}
