package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/pimessages"
)

const strictGrammarErrorV161 = "The compiled grammar is too large, which would cause performance issues. Simplify your tool schemas or reduce the number of strict tools."
const strictToolSchemaV161 = `{"type":"object","properties":{"value":{"type":"string"},"optional":{"type":"number"}},"required":["value"]}`
const strictSuccessSSEV161 = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"model\":\"claude-haiku-4-5\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

type strictFallbackPayloadV161 struct {
	Tools        []map[string]any `json:"tools"`
	Stream       bool             `json:"stream"`
	InferenceGeo string           `json:"inference_geo"`
}

type strictFallbackWireV161 struct {
	payloads []strictFallbackPayloadV161
}

func (w *strictFallbackWireV161) options(t *testing.T, reject func(int) string) goai.StreamOptions {
	t.Helper()
	return goai.StreamOptions{
		APIKey:      "test-key",
		RetryConfig: &goai.RetryConfig{MaxRetries: 0},
		HTTPTransport: anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
			defer r.Body.Close()
			if r.Method != http.MethodPost || r.URL.String() != "https://anthropic.example/v1/messages" {
				return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL)
			}
			var payload strictFallbackPayloadV161
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			w.payloads = append(w.payloads, payload)
			status, typ, body := 200, "text/event-stream", strictSuccessSSEV161
			if msg := reject(len(w.payloads)); msg != "" {
				status, typ = 400, "application/json"
				body = fmt.Sprintf(`{"type":"error","error":{"type":"invalid_request_error","message":%q}}`, msg)
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {typ}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	}
}

func strictFallbackFixtureV161(required, ordinary bool) (*goai.Model, *goai.Context) {
	model := &goai.Model{
		ID: "claude-haiku-4-5", Name: "Test Claude", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages,
		BaseURL: "https://anthropic.example", Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 100,
		AnthropicCompat: &goai.AnthropicMessagesCompat{SupportsStrictTools: boolPtrCompat(true)},
	}
	preferred := strictLookupTool(strictToolSchemaV161, "prefer")
	preferred.Name = "preferred"
	tools := []goai.Tool{preferred}
	if required {
		definition := strictLookupTool(strictToolSchemaV161, "require")
		definition.Name = "required"
		tools = append(tools, definition)
	}
	if ordinary {
		definition := strictLookupTool(strictToolSchemaV161, "")
		definition.Name = "ordinary"
		tools = append(tools, definition)
	}
	return model, &goai.Context{Tools: tools, Messages: []goai.Message{{
		Role: goai.RoleUser, Timestamp: 1, Content: []goai.ContentBlock{{Type: "text", Text: "Hello"}},
	}}}
}

type strictFallbackResultV161 struct {
	stop        goai.StopReason
	diagnostics []goai.AssistantMessageDiagnostic
	failure     error
	events      []string
}

func runStrictFallbackV161(ctx context.Context, model *goai.Model, input *goai.Context, opts goai.StreamOptions) strictFallbackResultV161 {
	var out strictFallbackResultV161
	for event := range goai.Stream(ctx, model, input, &opts) {
		switch e := event.(type) {
		case *goai.StartEvent:
			out.events = append(out.events, "start")
		case *goai.DoneEvent:
			out.events = append(out.events, "done")
			out.stop, out.diagnostics = e.Reason, e.Message.Diagnostics
		case *goai.ErrorEvent:
			out.events = append(out.events, "error")
			out.stop, out.failure = e.Reason, e.Err
			if e.Error != nil {
				out.diagnostics = e.Error.Diagnostics
			}
		default:
			out.events = append(out.events, fmt.Sprintf("%T", e))
		}
	}
	return out
}

func strictLimitFirstV161(message string) func(int) string {
	return func(attempt int) string {
		if attempt == 1 {
			return message
		}
		return ""
	}
}

func TestSourceV161AnthropicStrictFallbackResponses(t *testing.T) {
	for _, row := range []struct{ name, rejection string }{
		{"grammar", strictGrammarErrorV161},
		{"tool-count", "Too many strict tools (32). The maximum number of strict tools supported is 20. Try reducing the number of tools marked as strict."},
		{"union-count", "Schemas contains too many parameters with union types (480 parameters with type arrays or anyOf). This causes exponential compilation cost. Reduce the number of nullable or union-typed parameters (limit: 16 parameters with unions)."},
	} {
		t.Run(row.name, func(t *testing.T) {
			model, input := strictFallbackFixtureV161(true, true)
			original, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var wire strictFallbackWireV161
			opts := wire.options(t, strictLimitFirstV161(row.rejection))
			opts.CacheRetention = goai.CacheRetentionLong
			var calls int
			var statuses []int
			var hookOrder []string
			opts.OnPayload = func(payload any, _ *goai.Model) (any, error) {
				calls++
				hookOrder = append(hookOrder, "payload")
				raw, err := json.Marshal(payload)
				if err != nil {
					return nil, err
				}
				var override map[string]json.RawMessage
				if err := json.Unmarshal(raw, &override); err != nil {
					return nil, err
				}
				override["inference_geo"], override["stream"] = json.RawMessage(`"us"`), json.RawMessage(`false`)
				return override, nil
			}
			opts.OnResponse = func(status int, _ map[string]string, _ *goai.Model) {
				statuses = append(statuses, status)
				hookOrder = append(hookOrder, fmt.Sprintf("response:%d", status))
			}
			result := runStrictFallbackV161(t.Context(), model, input, opts)
			if result.stop != goai.StopReasonStop {
				t.Errorf("result = %+v, want stop", result)
			}
			if !slices.Equal(result.events, []string{"start", "done"}) {
				t.Errorf("events = %v, want [start done]", result.events)
			}
			if len(wire.payloads) != 2 {
				t.Fatalf("HTTP requests = %d, want 2", len(wire.payloads))
			}
			if calls != 2 {
				t.Errorf("payload hooks = %d, want 2", calls)
			}
			if !slices.Equal(statuses, []int{400, 200}) {
				t.Errorf("response statuses = %v, want [400 200]", statuses)
			}
			wantOrder := []string{"payload", "response:400", "payload", "response:200"}
			if !slices.Equal(hookOrder, wantOrder) {
				t.Errorf("hook order = %v, want %v", hookOrder, wantOrder)
			}
			first, second := wire.payloads[0], wire.payloads[1]
			if len(first.Tools) != 3 || len(second.Tools) != 3 {
				t.Fatalf("tool counts = %d / %d, want 3 on both", len(first.Tools), len(second.Tools))
			}
			strictSchema, _ := first.Tools[0]["input_schema"].(map[string]any)
			properties, _ := strictSchema["properties"].(map[string]any)
			strictOptional := map[string]any{"anyOf": []any{map[string]any{"type": "number"}, map[string]any{"type": "null"}}}
			if first.Tools[0]["strict"] != true || !reflect.DeepEqual(strictSchema["required"], []any{"value", "optional"}) || !reflect.DeepEqual(properties["optional"], strictOptional) {
				t.Errorf("initial preferred definition = %v", first.Tools[0])
			}
			if _, found := second.Tools[0]["strict"]; found {
				t.Errorf("fallback preferred strict flag still present: %v", second.Tools[0]["strict"])
			}
			var originalSchema map[string]any
			if err := json.Unmarshal([]byte(strictToolSchemaV161), &originalSchema); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(second.Tools[0]["input_schema"], originalSchema) {
				t.Errorf("fallback preferred input schema = %v, want %v", second.Tools[0]["input_schema"], originalSchema)
			}
			if !reflect.DeepEqual(second.Tools[1], first.Tools[1]) {
				t.Errorf("required changed from %v to %v", first.Tools[1], second.Tools[1])
			}
			if second.Tools[1]["strict"] != true {
				t.Errorf("required lost strict flag: %v", second.Tools[1])
			}
			if !reflect.DeepEqual(second.Tools[2], first.Tools[2]) {
				t.Errorf("ordinary tool changed from %v to %v", first.Tools[2], second.Tools[2])
			}
			if !reflect.DeepEqual(second.Tools[2]["cache_control"], map[string]any{"type": "ephemeral", "ttl": "1h"}) {
				t.Errorf("cache control = %v, want long retention", second.Tools[2]["cache_control"])
			}
			if !first.Stream || !second.Stream {
				t.Errorf("stream flags = [%t %t], want [true true]", first.Stream, second.Stream)
			}
			if second.InferenceGeo != "us" {
				t.Errorf("fallback inference_geo = %q", second.InferenceGeo)
			}
			after, err := json.Marshal(input)
			if err != nil || !slices.Equal(after, original) {
				t.Errorf("input changed: %s → %s (%v)", original, after, err)
			}
			if len(result.diagnostics) == 0 || result.diagnostics[0].Type != "anthropic_strict_tools_fallback" || result.diagnostics[0].Details["reason"] != "strict_tool_limits_exceeded" {
				t.Errorf("fallback diagnostic = %+v", result.diagnostics)
			}
		})
	}
}

func strictFlagsV161(payloads []strictFallbackPayloadV161) [][]any {
	traces := make([][]any, 0, len(payloads))
	for _, payload := range payloads {
		trace := make([]any, 0, len(payload.Tools))
		for _, tool := range payload.Tools {
			flag, present := tool["strict"]
			if present && flag == nil {
				flag = "null"
			}
			trace = append(trace, flag)
		}
		traces = append(traces, trace)
	}
	return traces
}

func TestSourceV161AnthropicStrictFallbackNoSession(t *testing.T) {
	model, input := strictFallbackFixtureV161(false, false)
	var wire strictFallbackWireV161
	opts := wire.options(t, strictLimitFirstV161(strictGrammarErrorV161))
	runStrictFallbackV161(t.Context(), model, input, opts)
	runStrictFallbackV161(t.Context(), model, input, opts)
	if got := strictFlagsV161(wire.payloads); !reflect.DeepEqual(got, [][]any{{true}, {nil}, {true}}) {
		t.Errorf("sessionless strict flags = %v", got)
	}
}

func strictSessionV161(t *testing.T, session string) string {
	t.Helper()
	id := t.Name() + ":" + session
	t.Cleanup(func() { cleanupStrictSessionV161(t, id) })
	return id
}

func cleanupStrictSessionV161(t *testing.T, session string) {
	t.Helper()
	if err := goai.CleanupSessionResources(session); err != nil {
		t.Fatal(err)
	}
}

func TestSourceV161AnthropicStrictFallbackRemembered(t *testing.T) {
	model, input := strictFallbackFixtureV161(true, false)
	var wire strictFallbackWireV161
	opts := wire.options(t, strictLimitFirstV161(strictGrammarErrorV161))
	opts.SessionID = strictSessionV161(t, "rejected")
	first := runStrictFallbackV161(t.Context(), model, input, opts)
	second := runStrictFallbackV161(t.Context(), model, input, opts)
	opts.SessionID = strictSessionV161(t, "other")
	runStrictFallbackV161(t.Context(), model, input, opts)
	if len(first.diagnostics) == 0 || first.diagnostics[0].Type != "anthropic_strict_tools_fallback" {
		t.Errorf("initial diagnostic = %+v", first.diagnostics)
	}
	if second.stop != goai.StopReasonStop {
		t.Errorf("remembered result = %+v", second)
	}
	if second.diagnostics != nil {
		t.Errorf("remembered request diagnostic = %+v, want nil", second.diagnostics)
	}
	want := [][]any{{true, true}, {nil, true}, {nil, true}, {true, true}}
	if got := strictFlagsV161(wire.payloads); !reflect.DeepEqual(got, want) {
		t.Errorf("remembered strict flags = %v, want %v", got, want)
	}
}

func TestSourceV161AnthropicStrictFallbackCleanup(t *testing.T) {
	model, input := strictFallbackFixtureV161(false, false)
	var wire strictFallbackWireV161
	opts := wire.options(t, strictLimitFirstV161(strictGrammarErrorV161))
	opts.SessionID = strictSessionV161(t, "cleaned")
	runStrictFallbackV161(t.Context(), model, input, opts)
	cleanupStrictSessionV161(t, opts.SessionID)
	runStrictFallbackV161(t.Context(), model, input, opts)
	if got := strictFlagsV161(wire.payloads); !reflect.DeepEqual(got, [][]any{{true}, {nil}, {true}}) {
		t.Errorf("strict flags after cleanup = %v", got)
	}
}

func TestSourceV161AnthropicStrictFallbackRememberedRequired(t *testing.T) {
	model, input := strictFallbackFixtureV161(true, false)
	var wire strictFallbackWireV161
	opts := wire.options(t, func(attempt int) string {
		if attempt == 2 {
			return ""
		}
		return strictGrammarErrorV161
	})
	opts.SessionID = strictSessionV161(t, "required")
	runStrictFallbackV161(t.Context(), model, input, opts)
	result := runStrictFallbackV161(t.Context(), model, input, opts)
	if result.stop != goai.StopReasonError {
		t.Errorf("required rejection result = %+v", result)
	}
	if result.failure == nil || !strings.Contains(result.failure.Error(), strictGrammarErrorV161) {
		t.Errorf("required rejection text = %v", result.failure)
	}
	if len(wire.payloads) != 3 {
		t.Errorf("required rejection attempt count = %d, want 3", len(wire.payloads))
	}
}

func TestSourceV161AnthropicStrictFallbackHookException(t *testing.T) {
	model, input := strictFallbackFixtureV161(false, false)
	var wire strictFallbackWireV161
	opts := wire.options(t, func(int) string { return "" })
	opts.OnPayload = func(any, *goai.Model) (any, error) {
		return nil, &goai.ProviderRequestError{Status: 400, Message: `{"type":"invalid_request_error","message":"` + strictGrammarErrorV161 + `"}`}
	}
	result := runStrictFallbackV161(t.Context(), model, input, opts)
	if result.stop != goai.StopReasonError {
		t.Errorf("hook exception result = %+v", result)
	}
	if len(wire.payloads) != 0 {
		t.Errorf("hook exception made %d requests", len(wire.payloads))
	}
	if result.diagnostics != nil {
		t.Errorf("hook exception had fallback diagnostics: %+v", result.diagnostics)
	}
}

// The response hook added by the fallback must not make cancellation record a
// rejection for a later request or trigger another attempt.
func TestAnthropicStrictFallbackResponseAbortV161(t *testing.T) {
	model, input := strictFallbackFixtureV161(false, false)
	var wire strictFallbackWireV161
	opts := wire.options(t, strictLimitFirstV161(strictGrammarErrorV161))
	opts.SessionID = strictSessionV161(t, "aborted")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opts.OnResponse = func(int, map[string]string, *goai.Model) { cancel() }
	result := runStrictFallbackV161(ctx, model, input, opts)
	if result.stop != goai.StopReasonAborted || len(wire.payloads) != 1 {
		t.Fatalf("after hook cancellation: result=%+v requests=%d", result, len(wire.payloads))
	}
	opts.OnResponse = nil
	runStrictFallbackV161(t.Context(), model, input, opts)
	if got := strictFlagsV161(wire.payloads); !reflect.DeepEqual(got, [][]any{{true}, {true}}) {
		t.Errorf("strict flags after hook cancellation = %v, want both strict", got)
	}
}

func TestAnthropicStrictToolFallbackPiMessagesPublicAdapter(t *testing.T) {
	model, input := strictFallbackFixtureV161(false, false)
	var wire strictFallbackWireV161
	anthropicOpts := wire.options(t, strictLimitFirstV161(strictGrammarErrorV161))
	gateway := &goai.Model{ID: "gateway-claude", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "https://radius.example/v1"}
	var gatewayContext json.RawMessage
	gatewayOpts := goai.StreamOptions{
		APIKey: "radius-test",
		HTTPTransport: anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
			defer r.Body.Close()
			if r.Method != http.MethodPost || r.URL.String() != "https://radius.example/v1/messages" {
				return nil, fmt.Errorf("unexpected gateway request %s %s", r.Method, r.URL)
			}
			var request struct {
				Context json.RawMessage `json:"context"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				return nil, err
			}
			gatewayContext = append(json.RawMessage(nil), request.Context...)
			var forwarded goai.Context
			if err := json.Unmarshal(request.Context, &forwarded); err != nil {
				return nil, err
			}
			upstream := runStrictFallbackV161(t.Context(), model, &forwarded, anthropicOpts)
			if upstream.stop != goai.StopReasonStop {
				return nil, fmt.Errorf("upstream stop reason %q", upstream.stop)
			}
			body := `data: {"type":"done","reason":"stop","usage":{"input":10,"output":1,"totalTokens":11,"cost":{"total":0}}}` + "\n\n"
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	}

	result := runStrictFallbackV161(t.Context(), gateway, input, gatewayOpts)
	if result.stop != goai.StopReasonStop {
		t.Fatalf("gateway result = %+v", result)
	}
	if got := strictFlagsV161(wire.payloads); !reflect.DeepEqual(got, [][]any{{true}, {nil}}) {
		t.Errorf("gateway Anthropic strict flags = %v, want [[true] [nil]]", got)
	}
	original, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(original, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(gatewayContext, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pi-messages context = %s, want %s", gatewayContext, original)
	}
}

// The source cap is FIFO; an existing entry does not move to the newest slot.
func TestAnthropicStrictFallbackSessionCapV161(t *testing.T) {
	cleanupStrictSessionV161(t, "")
	t.Cleanup(func() { cleanupStrictSessionV161(t, "") })
	for i := range 10_000 {
		rememberStrictToolLimitRejection(fmt.Sprintf("cap-%d", i))
	}
	rememberStrictToolLimitRejection("cap-0")
	rememberStrictToolLimitRejection("new")
	if hasStrictToolLimitRejection("cap-0") || !hasStrictToolLimitRejection("cap-1") || !hasStrictToolLimitRejection("new") {
		t.Fatal("session cache did not evict just the oldest session")
	}
	cleanupStrictSessionV161(t, "cap-1")
	if hasStrictToolLimitRejection("cap-1") || !hasStrictToolLimitRejection("new") {
		t.Fatal("targeted cleanup affected the wrong session")
	}
	cleanupStrictSessionV161(t, "")
	if hasStrictToolLimitRejection("new") || hasStrictToolLimitRejection("cap-2") {
		t.Fatal("all-session cleanup did not clear remembered sessions")
	}
}
