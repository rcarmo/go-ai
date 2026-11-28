package google

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

var errGoogleV170PayloadCaptured = errors.New("payload captured")

func captureGoogleV170Payload(t *testing.T, model *goai.Model, transcript *goai.Context) geminiRequest {
	t.Helper()
	var payload geminiRequest
	hookCalls := 0
	transportCalls := 0
	transport := vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		transportCalls++
		return nil, errors.New("unexpected transport call")
	})
	opts := &goai.StreamOptions{
		APIKey:        "test-key",
		HTTPTransport: transport,
		OnPayload: func(value any, _ *goai.Model) (any, error) {
			hookCalls++
			wire, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(wire, &payload); err != nil {
				return nil, err
			}
			return nil, errGoogleV170PayloadCaptured
		},
	}
	var terminal error
	for event := range goai.GetApiProvider(model.Api).StreamSimple(t.Context(), model, transcript, opts) {
		if failure, ok := event.(*goai.ErrorEvent); ok {
			terminal = failure.Err
		}
	}
	if !errors.Is(terminal, errGoogleV170PayloadCaptured) || hookCalls != 1 || transportCalls != 0 {
		t.Fatalf("capture: hook=%d transport=%d terminal=%v", hookCalls, transportCalls, terminal)
	}
	return payload
}

func TestGoogleUnchangedV170ImageToolRoutingPublicPayload(t *testing.T) {
	for _, tc := range []struct {
		name    string
		modelID string
		turns   int
	}{
		{"gemini-2", "gemini-2.5-flash", 5},
		{"gemini-3", "gemini-3-pro-preview", 3},
		{"prefixed-gemini-4", "randomprefix-gemini-4-argon", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := makeGoogleImageRoutingModel(tc.modelID)
			payload := captureGoogleV170Payload(t, model, makeGoogleImageRoutingContext(model))
			if len(payload.Contents) != tc.turns {
				t.Fatalf("contents=%d want %d", len(payload.Contents), tc.turns)
			}
			if tc.turns == 5 {
				if len(payload.Contents[3].Parts) != 2 || payload.Contents[3].Parts[0].Text != "Tool result image:" || payload.Contents[3].Parts[1].InlineData == nil {
					t.Fatalf("synthetic image turn=%#v", payload.Contents[3])
				}
				return
			}
			response := payload.Contents[2].Parts[1].FunctionResponse
			if response == nil || len(response.Parts) != 1 || response.Parts[0].InlineData == nil {
				t.Fatalf("nested image response=%#v", response)
			}
		})
	}
}

func TestGoogleUnchangedV170WhitespaceOnlyToolTextPublicPayload(t *testing.T) {
	model := makeGoogleImageRoutingModel("gemini-3-pro-preview")
	transcript := makeGoogleImageRoutingContext(model)
	for i := range transcript.Messages {
		message := &transcript.Messages[i]
		if message.Role != goai.RoleToolResult {
			continue
		}
		if message.ToolCallID == "call_a" {
			message.Content = []goai.ContentBlock{{Type: "text", Text: "\r\n"}}
		}
		if message.ToolCallID == "call_img" {
			message.Content = append([]goai.ContentBlock{{Type: "text", Text: " \n"}}, message.Content...)
		}
	}
	payload := captureGoogleV170Payload(t, model, transcript)
	var outputs []any
	for _, part := range payload.Contents[2].Parts {
		if part.FunctionResponse != nil {
			outputs = append(outputs, part.FunctionResponse.Response["output"])
		}
	}
	want := []any{"", "(see attached image)", "beta text"}
	if !reflect.DeepEqual(outputs, want) {
		t.Fatalf("outputs=%#v want %#v", outputs, want)
	}
}

func TestGoogleUnchangedV170HeadersLessRetryErrorObservation(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "limited")
	}))
	defer server.Close()
	model := makeGoogleImageRoutingModel("gemini-2.5-flash")
	model.BaseURL = server.URL
	var statuses []int
	var headerMaps []map[string]string
	opts := &goai.StreamOptions{APIKey: "test-key", OnResponse: func(status int, headers map[string]string, _ *goai.Model) {
		statuses = append(statuses, status)
		headerMaps = append(headerMaps, headers)
	}}
	transcript := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	var terminal *goai.ErrorEvent
	for event := range goai.GetApiProvider(goai.ApiGoogleGenerativeAI).Stream(t.Context(), model, transcript, opts) {
		if failure, ok := event.(*goai.ErrorEvent); ok {
			terminal = failure
		}
	}
	if requests != 1 || terminal == nil || terminal.Err == nil || !strings.Contains(terminal.Err.Error(), "HTTP 429") {
		t.Fatalf("requests=%d terminal=%#v", requests, terminal)
	}
	if !reflect.DeepEqual(statuses, []int{http.StatusTooManyRequests}) || len(headerMaps) != 1 || len(headerMaps[0]) != 0 {
		t.Fatalf("responses=%v headers=%v", statuses, headerMaps)
	}
}
