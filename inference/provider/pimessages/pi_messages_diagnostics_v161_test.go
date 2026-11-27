package pimessages

import (
	"io"
	"net/http"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161PiMessagesDebugHeaders(t *testing.T) {
	model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid/v1"}
	var path, observed string
	transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		path = r.URL.RequestURI()
		resp := piMessagesV161Response([]string{`{"type":"done","reason":"stop","usage":` + piMessagesV161Usage + `}`})
		resp.Header.Set("X-Pi-Gateway-Upstream-Provider", "anthropic")
		return resp, nil
	})
	result, err := goai.Complete(t.Context(), model, &goai.Context{}, &goai.StreamOptions{
		APIKey: "test-key", Metadata: map[string]any{"debug": true}, HTTPTransport: transport,
		OnResponse: func(_ int, headers map[string]string, _ *goai.Model) {
			observed = headers["x-pi-gateway-upstream-provider"]
		},
	})
	if err != nil || result == nil || result.StopReason != goai.StopReasonStop {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if path != "/v1/messages?debug=1" {
		t.Errorf("path=%q", path)
	}
	if observed != "anthropic" {
		t.Errorf("upstream header=%q", observed)
	}
}

func TestSourceV161PiMessagesBackendError(t *testing.T) {
	model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid/v1"}
	transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return &http.Response{StatusCode: 401, Status: "401 Unauthorized", Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Token expired","code":"unauthorized"}}`))}, nil
	})
	result, err := goai.Complete(t.Context(), model, &goai.Context{}, &goai.StreamOptions{APIKey: "stale", HTTPTransport: transport})
	if err == nil || result == nil || result.StopReason != goai.StopReasonError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, part := range []string{"401", "Token expired", "unauthorized"} {
		if !strings.Contains(result.ErrorMessage, part) {
			t.Errorf("error %q does not contain %q", result.ErrorMessage, part)
		}
	}
	if len(result.Diagnostics) == 0 || result.Diagnostics[0].Type != "pi_messages_response_failure" {
		t.Fatalf("diagnostics=%#v", result.Diagnostics)
	}
	if status := result.Diagnostics[0].Details["status"]; status != 401 {
		t.Errorf("diagnostic status=%#v", status)
	}
}

func TestSourceV161PiMessagesMissingKey(t *testing.T) {
	model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://127.0.0.1:1/v1"}
	transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
		r.Body.Close()
		t.Error("transport called with no credentials")
		return nil, io.EOF
	})
	result, err := goai.Complete(t.Context(), model, &goai.Context{}, &goai.StreamOptions{
		HTTPTransport: transport, Env: goai.ProviderEnv{"RADIUS_API_KEY": "", "PI_MESSAGES_API_KEY": ""},
	})
	if err == nil || result == nil || result.StopReason != goai.StopReasonError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !strings.Contains(result.ErrorMessage, "No API key provided") {
		t.Errorf("error=%q", result.ErrorMessage)
	}
}

func TestSourceV161PiMessagesMissingTerminal(t *testing.T) {
	model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid/v1"}
	transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		return piMessagesV161Response([]string{`{"type":"start"}`, `{"type":"text_start","contentIndex":0}`, `{"type":"text_delta","contentIndex":0,"delta":"partial"}`}), nil
	})
	result, err := goai.Complete(t.Context(), model, &goai.Context{}, &goai.StreamOptions{APIKey: "test-key", HTTPTransport: transport})
	if err == nil || result == nil || result.StopReason != goai.StopReasonError {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !strings.Contains(result.ErrorMessage, "stream ended without a terminal event") {
		t.Errorf("error=%q", result.ErrorMessage)
	}
}

func TestSourceV161PiMessagesBuiltin(t *testing.T) {
	if goai.GetApiProvider("pi-messages") == nil {
		t.Fatal("pi-messages provider is not registered")
	}
}

func TestSourceV161PiMessagesKnownAPI(t *testing.T) {
	model := goai.Model{Api: goai.ApiPiMessages}
	if model.Api != "pi-messages" {
		t.Errorf("api=%q", model.Api)
	}
}
