package google

// Port of google-raw-stop-reason.test.ts at 81f3cd41. The SDK generator mock
// becomes loopback HTTP/SSE; Vertex uses an explicit fake key (ADC is separate).
// SDK object reference identity has no wire equivalent and is not asserted.
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func captureMainGoogleStream(t *testing.T, api goai.Api, chunks []string, configure func(*goai.StreamOptions, *goai.Model)) (*goai.Message, http.Header) {
	t.Helper()
	message, headers, _ := captureMainGoogleStreamEvents(t, api, chunks, configure)
	return message, headers
}

func captureMainGoogleStreamEvents(t *testing.T, api goai.Api, chunks []string, configure func(*goai.StreamOptions, *goai.Model)) (*goai.Message, http.Header, []goai.Event) {
	t.Helper()
	var wire strings.Builder
	for _, chunk := range chunks {
		fmt.Fprintf(&wire, "data: %s\n\n", chunk)
	}
	return captureMainGoogleWireEvents(t, api, wire.String(), configure)
}

func captureMainGoogleWireEvents(t *testing.T, api goai.Api, wire string, configure func(*goai.StreamOptions, *goai.Model)) (*goai.Message, http.Header, []goai.Event) {
	t.Helper()
	headers := make(chan http.Header, 1)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case headers <- r.Header.Clone():
		default:
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, wire)
	}))
	defer server.Close()
	id := "gemini-2.5-flash"
	if api == goai.ApiGoogleVertex {
		id = "gemini-3-flash-preview"
	}
	model := upstreamGoogleThinkingModel(api, id, nil)
	model.BaseURL = server.URL
	opts := &goai.StreamOptions{APIKey: "test-api-key", Project: "test-project", Location: "us-central1"}
	if configure != nil {
		configure(opts, model)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var message *goai.Message
	var events []goai.Event
	for event := range goai.GetApiProvider(api).Stream(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
		events = append(events, event)
		switch e := event.(type) {
		case *goai.DoneEvent:
			message = e.Message
		case *goai.ErrorEvent:
			message = e.Error
			if message == nil {
				t.Fatalf("stream failed before response: %v", e.Err)
			}
		}
	}
	if message == nil {
		t.Fatal("no terminal message")
	}
	if calls.Load() != 1 {
		t.Fatalf("HTTP requests %d want 1", calls.Load())
	}
	return message, <-headers, events
}
func mainGoogleStopChunk(reason string, tool bool) string {
	candidate := map[string]any{"finishReason": reason}
	if tool {
		candidate["content"] = map[string]any{"parts": []any{map[string]any{"functionCall": map[string]any{"id": "call-1", "name": "echo", "args": map[string]any{"value": "truncated"}}}}}
	}
	raw, err := json.Marshal(map[string]any{"responseId": "google-response-id", "candidates": []any{candidate}, "usageMetadata": map[string]any{"promptTokenCount": 1, "candidatesTokenCount": 0, "totalTokenCount": 1}})
	if err != nil {
		panic(err)
	}
	return string(raw)
}
func TestUpstreamMainGoogleRawStopErrors(t *testing.T) {
	for _, tc := range []struct {
		api goai.Api
		raw string
	}{{goai.ApiGoogleGenerativeAI, "MALFORMED_FUNCTION_CALL"}, {goai.ApiGoogleVertex, "SAFETY"}} {
		t.Run(string(tc.api), func(t *testing.T) {
			message, _ := captureMainGoogleStream(t, tc.api, []string{mainGoogleStopChunk(tc.raw, false)}, nil)
			if message.StopReason != goai.StopReasonError || message.RawStopReason != tc.raw || message.ErrorMessage != "Provider stopped with: "+tc.raw {
				t.Fatalf("message %#v", message)
			}
		})
	}
}
func TestUpstreamMainGoogleRawStopTools(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			raw  string
			want goai.StopReason
		}{{"MAX_TOKENS", goai.StopReasonLength}, {"STOP", goai.StopReasonToolUse}} {
			t.Run(string(api)+"/"+tc.raw, func(t *testing.T) {
				message, _ := captureMainGoogleStream(t, api, []string{mainGoogleStopChunk(tc.raw, true)}, nil)
				hasTool := false
				for _, b := range message.Content {
					if b.Type == "toolCall" {
						hasTool = true
					}
				}
				if message.StopReason != tc.want || message.RawStopReason != tc.raw || !hasTool {
					t.Fatalf("reason=%s raw=%s tool=%t", message.StopReason, message.RawStopReason, hasTool)
				}
			})
		}
	}
}
func TestUpstreamMainGoogleProviderEvents(t *testing.T) {
	chunks := []string{`{"responseId":"resp_google","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`, `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`}
	var want []any
	for _, raw := range chunks {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		want = append(want, value)
	}
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		t.Run(string(api), func(t *testing.T) {
			var received []any
			var models []*goai.Model
			var expected *goai.Model
			message, _ := captureMainGoogleStream(t, api, chunks, func(opts *goai.StreamOptions, model *goai.Model) {
				expected = model
				opts.OnProviderStreamEvent = func(value any, eventModel *goai.Model) error {
					received = append(received, value)
					models = append(models, eventModel)
					return nil
				}
			})
			if !reflect.DeepEqual(received, want) {
				t.Errorf("chunks %#v want %#v", received, want)
			}
			if !reflect.DeepEqual(models, []*goai.Model{expected, expected}) {
				t.Errorf("event models %#v", models)
			}
			if message.StopReason != goai.StopReasonStop || message.ResponseID != "resp_google" || len(message.Content) != 1 {
				t.Fatalf("message %#v", message)
			}
			if !reflect.DeepEqual(message.Content, []goai.ContentBlock{{Type: "text", Text: "hello"}}) {
				t.Fatalf("content %#v", message.Content)
			}
		})
	}
}
func TestUpstreamMainGoogleUserAgent(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%t", explicit), func(t *testing.T) {
			want := goai.FormatUserAgent(nil)
			if explicit {
				want = "custom-agent"
			}
			_, headers := captureMainGoogleStream(t, goai.ApiGoogleGenerativeAI, []string{mainGoogleStopChunk("STOP", false)}, func(opts *goai.StreamOptions, _ *goai.Model) {
				if explicit {
					opts.Headers = map[string]string{"User-Agent": "custom-agent"}
				}
			})
			if headers.Get("User-Agent") != want {
				t.Fatalf("User-Agent %q want %q", headers.Get("User-Agent"), want)
			}
		})
	}
}

// Source: google-stop-reason-continuation.test.ts and the Google cases in
// fetch-option.test.ts. RoundTripper is the native custom fetch seam.
func TestSourceV160GoogleContinuationAndHTTPTransport(t *testing.T) {
	if got := mapFinishReason("CONTINUATION"); got != goai.StopReasonLength {
		t.Errorf("CONTINUATION = %s, want length", got)
	}
	for _, mode := range []string{"custom", "ambient"} {
		t.Run(mode, func(t *testing.T) {
			var fallback atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fallback.Add(1)
				http.Error(w, "custom transport was bypassed", http.StatusBadRequest)
			}))
			defer server.Close()
			var requests atomic.Int32
			wire := vertexTokenWireFunc(func(req *http.Request) (*http.Response, error) {
				requests.Add(1)
				if !strings.HasPrefix(req.URL.String(), server.URL+"/v1/") {
					t.Errorf("transport URL = %s", req.URL)
				}
				return &http.Response{Request: req, StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"partial\"}]},\"finishReason\":\"CONTINUATION\",\"continuationToken\":\"token-1\"}]}\n\n"))}, nil
			})
			if mode == "ambient" {
				old := http.DefaultTransport
				http.DefaultTransport = wire
				t.Cleanup(func() { http.DefaultTransport = old })
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
				model := upstreamGoogleThinkingModel(api, "gemini-test", nil)
				model.BaseURL = server.URL + "/v1"
				opts := &goai.StreamOptions{APIKey: "test-key"}
				if mode == "custom" {
					opts.HTTPTransport = wire
				}
				var done *goai.DoneEvent
				var errors []string
				for event := range goai.GetApiProvider(api).Stream(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done = e
					case *goai.ErrorEvent:
						errors = append(errors, fmt.Sprint(e.Err))
					}
				}
				if done == nil || done.Reason != goai.StopReasonLength || done.Message.RawStopReason != "CONTINUATION" || len(done.Message.Content) != 1 || done.Message.Content[0].Text != "partial" || len(errors) != 0 {
					t.Errorf("%s: terminal=%+v, errors=%v", api, done, errors)
				}
			}
			if requests.Load() != 2 || fallback.Load() != 0 {
				t.Errorf("transport requests=%d, bypasses=%d; want 2 and 0", requests.Load(), fallback.Load())
			}
		})
	}
}
