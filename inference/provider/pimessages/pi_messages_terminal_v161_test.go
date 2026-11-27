package pimessages

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const piMessagesV161Usage = `{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":15,"cost":{"input":0.1,"output":0.2,"cacheRead":0,"cacheWrite":0,"total":0.3}}`

func TestSourceV161PiMessagesTerminalMetadata(t *testing.T) {
	frames := []string{
		`{"type":"start"}`,
		`{"type":"text_start","contentIndex":0}`,
		`{"type":"text_delta","contentIndex":0,"delta":"Hel"}`,
		`{"type":"text_delta","contentIndex":0,"delta":"lo"}`,
		`{"type":"text_end","contentIndex":0,"content":"Hello"}`,
		`{"type":"toolcall_start","contentIndex":1,"id":"call_1","toolName":"read"}`,
		`{"type":"toolcall_delta","contentIndex":1,"delta":"{\"path\":"}`,
		`{"type":"toolcall_delta","contentIndex":1,"delta":"\"a.txt\"}"}`,
		`{"type":"toolcall_end","contentIndex":1,"toolCall":{"type":"toolCall","id":"call_1","name":"read","arguments":{"path":"a.txt"}}}`,
		`{"type":"done","reason":"toolUse","usage":` + piMessagesV161Usage + `,"responseId":"resp_1","responseModel":"claude-opus-5-5","providerThinkingLevel":"high"}`,
	}
	model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid/v1"}
	var reqs int
	var path, authorization, custom string
	var body map[string]any
	transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		reqs++
		path, authorization, custom = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Custom")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		return piMessagesV161Response(frames), nil
	})
	u := goai.UserMessage("Hello")
	u.Timestamp = 123
	ctx := &goai.Context{Messages: []goai.Message{u}}
	max := 100
	var startReason goai.StopReason
	var result *goai.Message
	var toolEnds int
	var textDelta bool
	for evt := range goai.GetApiProvider(model.Api).Stream(t.Context(), model, ctx, &goai.StreamOptions{
		APIKey: "test-key", SessionID: "session-1", ToolChoice: goai.ToolChoiceAuto, MaxTokens: &max, Headers: map[string]string{"x-custom": "1"}, HTTPTransport: transport,
	}) {
		switch e := evt.(type) {
		case *goai.StartEvent:
			if startReason == "" {
				startReason = e.Partial.StopReason
			}
		case *goai.TextDeltaEvent:
			textDelta = true
		case *goai.ToolCallEndEvent:
			toolEnds++
		case *goai.DoneEvent:
			result = e.Message
		case *goai.ErrorEvent:
			t.Fatalf("unexpected stream error: %v", e.Err)
		}
	}
	if result == nil {
		t.Fatal("no terminal message")
	}
	if startReason != goai.StopReasonPending {
		t.Errorf("initial stop reason = %q; want pending", startReason)
	}
	if result.StopReason != goai.StopReasonToolUse {
		t.Errorf("stop reason = %q; want toolUse", result.StopReason)
	}
	wantUsage := &goai.Usage{Input: 10, Output: 5, TotalTokens: 15, Cost: goai.CostBreakdown{Input: .1, Output: .2, Total: .3}}
	if !reflect.DeepEqual(result.Usage, wantUsage) {
		t.Errorf("usage = %#v; want %#v", result.Usage, wantUsage)
	}
	if result.ResponseID != "resp_1" || result.ProviderThinkingLevel != "high" || result.Model != "auto" || result.ResponseModel != "claude-opus-5-5" || result.Provider != "radius" {
		t.Errorf("terminal identity: ID %q level %q model %q response model %q provider %q", result.ResponseID, result.ProviderThinkingLevel, result.Model, result.ResponseModel, result.Provider)
	}
	wantContent := []goai.ContentBlock{{Type: "text", Text: "Hello"}, {Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "a.txt"}}}
	if !reflect.DeepEqual(result.Content, wantContent) {
		t.Errorf("content = %#v; want %#v", result.Content, wantContent)
	}
	if !textDelta || toolEnds != 1 {
		t.Errorf("text-delta=%v, tool ends=%d", textDelta, toolEnds)
	}
	if reqs != 1 || path != "/v1/messages" || authorization != "Bearer test-key" || custom != "1" {
		t.Errorf("request count %d, path %q, authorization %q, custom %q", reqs, path, authorization, custom)
	}
	// The Go Context always represents user content as blocks, not a string.
	var wantBody map[string]any
	if err := json.Unmarshal([]byte(`{"model":"auto","context":{"messages":[{"role":"user","content":[{"type":"text","text":"Hello"}],"timestamp":123}]},"options":{"maxTokens":100,"sessionId":"session-1","toolChoice":"auto"}}`), &wantBody); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, wantBody) {
		t.Errorf("body = %#v; want %#v", body, wantBody)
	}
}

func TestSourceV161PiMessagesForwardsExactEvents(t *testing.T) {
	frames := []string{`{"type":"start"}`, `{"type":"text_start","contentIndex":0}`, `{"type":"text_delta","contentIndex":0,"delta":"Hello","gatewayField":"upstream-value"}`, `{"type":"text_end","contentIndex":0,"content":"Hello"}`, `{"type":"done","reason":"stop","usage":` + piMessagesV161Usage + `,"responseId":"resp_1"}`}
	model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid/v1"}
	var observed []any
	var models []*goai.Model
	transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return piMessagesV161Response(frames), nil
	})
	result, err := goai.Complete(t.Context(), model, &goai.Context{}, &goai.StreamOptions{
		APIKey: "test-key", HTTPTransport: transport, OnProviderStreamEvent: func(event any, m *goai.Model) error {
			observed = append(observed, event)
			models = append(models, m)
			return nil
		},
	})
	if err != nil || result == nil || result.StopReason != goai.StopReasonStop || result.ResponseID != "resp_1" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	var want []any
	if err := json.Unmarshal([]byte("["+strings.Join(frames, ",")+"]"), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observed, want) {
		t.Errorf("raw events = %#v; want %#v", observed, want)
	}
	if len(models) != len(frames) {
		t.Fatalf("hook model count = %d; want %d", len(models), len(frames))
	}
	for _, m := range models {
		if m != model {
			t.Error("hook got a different model")
		}
	}
	var projected map[string]json.RawMessage
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(wire, &projected); err != nil {
		t.Fatal(err)
	}
	if _, present := projected["responseModel"]; present {
		t.Error("responseModel was not omitted")
	}
	if !reflect.DeepEqual(result.Content, []goai.ContentBlock{{Type: "text", Text: "Hello"}}) {
		t.Errorf("content=%#v", result.Content)
	}
}

func TestSourceV161PiMessagesServerError(t *testing.T) {
	model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid/v1"}
	transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return piMessagesV161Response([]string{
			`{"type":"start"}`,
			`{"type":"error","reason":"error","usage":` + piMessagesV161Usage + `,"errorMessage":"Upstream failed","responseModel":"claude-opus-5-5"}`,
		}), nil
	})
	result, err := goai.Complete(t.Context(), model, &goai.Context{}, &goai.StreamOptions{APIKey: "test-key", HTTPTransport: transport})
	if err == nil || result == nil || result.StopReason != goai.StopReasonError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if result.ErrorMessage != "Upstream failed" {
		t.Errorf("message=%q", result.ErrorMessage)
	}
	wantUsage := &goai.Usage{Input: 10, Output: 5, TotalTokens: 15, Cost: goai.CostBreakdown{Input: .1, Output: .2, Total: .3}}
	if !reflect.DeepEqual(result.Usage, wantUsage) {
		t.Errorf("error usage = %#v; want %#v", result.Usage, wantUsage)
	}
	if result.ResponseModel != "claude-opus-5-5" {
		t.Errorf("error response model = %q", result.ResponseModel)
	}
}

func piMessagesV161Response(frames []string) *http.Response {
	wire := "data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
}
