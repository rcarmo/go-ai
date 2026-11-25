package google

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

const googleContinuationBaseV160 = "https://upstream.test/v1"
const googleContinuationFrameV160 = `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]},"finishReason":"CONTINUATION","continuationToken":"token-1"}]}` + "\n\n"

type googleContinuationRequestV160 struct {
	method, url string
	key         []string
	body        map[string]any
}

func googleContinuationModelV160(api goai.Api) *goai.Model {
	return &goai.Model{ID: "test-model", Name: "Test Model", Provider: "test-provider", Api: api, BaseURL: googleContinuationBaseV160,
		Input: []string{"text"}, ContextWindow: 10_000, MaxTokens: 1_000}
}

func googleContinuationContextV160() *goai.Context {
	user := goai.UserMessage("hello")
	user.Timestamp = 1
	return &goai.Context{Messages: []goai.Message{user}}
}

func googleContinuationTransportV160(requests *[]googleContinuationRequestV160) http.RoundTripper {
	return vertexTokenWireFunc(func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		if req.URL.Scheme != "https" || req.URL.Host != "upstream.test" {
			return nil, fmt.Errorf("request escaped continuation fixture authority: %v", req.URL)
		}
		data, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			return nil, err
		}
		*requests = append(*requests, googleContinuationRequestV160{req.Method, req.URL.String(), req.Header.Values("X-Goog-Api-Key"), body})
		return &http.Response{
			Request: req, StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(googleContinuationFrameV160)),
		}, nil
	})
}

func guardGoogleContinuationNetworkV160(t *testing.T) {
	t.Helper()
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = vertexTokenWireFunc(func(*http.Request) (*http.Response, error) {
		t.Error("Google continuation bypassed the fixture transport")
		return nil, fmt.Errorf("unexpected request outside the continuation fixture transport")
	})
}

func checkGoogleContinuationEventsV160(t *testing.T, api goai.Api, events <-chan goai.Event) {
	t.Helper()
	var order []string
	for event := range events {
		switch got := event.(type) {
		case *goai.StartEvent:
			order = append(order, "start")
		case *goai.TextStartEvent:
			order = append(order, "text_start")
			if got.ContentIndex != 0 {
				t.Errorf("%s text_start index = %d, want 0", api, got.ContentIndex)
			}
		case *goai.TextDeltaEvent:
			order = append(order, "text_delta")
			if got.ContentIndex != 0 || got.Delta != "partial" {
				t.Errorf("%s text_delta = %q at %d, want partial at 0", api, got.Delta, got.ContentIndex)
			}
		case *goai.TextEndEvent:
			order = append(order, "text_end")
			if got.ContentIndex != 0 || got.Content != "partial" {
				t.Errorf("%s text_end = %q at %d, want partial at 0", api, got.Content, got.ContentIndex)
			}
		case *goai.DoneEvent:
			order = append(order, "done:"+string(got.Reason))
			message := got.Message
			if got.Reason != goai.StopReasonLength || message == nil || message.StopReason != goai.StopReasonLength || message.RawStopReason != "CONTINUATION" ||
				len(message.Content) != 1 || message.Content[0].Type != "text" || message.Content[0].Text != "partial" {
				t.Errorf("%s done reason = %q message = %#v, want length/CONTINUATION and partial text", api, got.Reason, message)
			}
		default:
			order = append(order, fmt.Sprintf("%T", event))
			t.Errorf("%s unexpected continuation event: %#v", api, event)
		}
	}
	if want := []string{"start", "text_start", "text_delta", "text_end", "done:length"}; !slices.Equal(order, want) {
		t.Errorf("%s event order = %v, want %v", api, order, want)
	}
}

func googleContinuationBodyV160(outputTokens *int) map[string]any {
	body := map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]any{"text": "hello"}}, "role": "user"}}}
	if outputTokens != nil {
		body["generationConfig"] = map[string]any{"maxOutputTokens": float64(*outputTokens)}
	}
	return body
}

// Source: pi v1.6.0 packages/ai/test/google-stop-reason-continuation.test.ts:30–62.
func TestSourceV160GoogleContinuationWire(t *testing.T) {
	if got := mapFinishReason("CONTINUATION"); got != goai.StopReasonLength {
		t.Errorf("CONTINUATION = %s, want length", got)
	}
	guardGoogleContinuationNetworkV160(t)
	var requests []googleContinuationRequestV160
	opts := &goai.StreamOptions{APIKey: "test-key", HTTPTransport: googleContinuationTransportV160(&requests)}
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		model := googleContinuationModelV160(api)
		checkGoogleContinuationEventsV160(t, api, goai.GetApiProvider(api).StreamSimple(t.Context(), model, googleContinuationContextV160(), opts))
	}
	if len(requests) != 2 {
		t.Fatalf("continuation requests = %d, want 2", len(requests))
	}
	tokens := 1000
	for i, wantURL := range []string{
		googleContinuationBaseV160 + "/models/test-model:streamGenerateContent?alt=sse",
		googleContinuationBaseV160 + "/publishers/google/models/test-model:streamGenerateContent?alt=sse",
	} {
		got := requests[i]
		target := got.url
		if i == 0 {
			// Standalone Google still uses the query key. This comparison
			// excludes only that known SDK auth difference, not other query fields.
			target = strings.TrimSuffix(target, "&key=test-key")
		} else if !slices.Equal(got.key, []string{"test-key"}) {
			t.Errorf("Vertex x-goog-api-key = %v, want one test-key", got.key)
		}
		if got.method != http.MethodPost || target != wantURL {
			t.Errorf("request %d: %s %s, want POST %s", i, got.method, target, wantURL)
		}
		if want := googleContinuationBodyV160(&tokens); !reflect.DeepEqual(got.body, want) {
			t.Errorf("request %d continuation body = %#v, want %#v", i, got.body, want)
		}
	}
}

// Supplemental: the model default applies only to simple dispatch and must
// not replace either a caller cap or its explicit zero.
func TestGoogleSimpleContinuationTokenPolicyV160(t *testing.T) {
	guardGoogleContinuationNetworkV160(t)
	explicit, zero, clamped := 350, 0, 902
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		provider := goai.GetApiProvider(api)
		for _, tc := range []struct {
			name           string
			stream         goai.ProviderStream
			provided, want *int
			contextWindow  int
		}{
			{"explicit", provider.StreamSimple, &explicit, &explicit, 10_000},
			{"explicit-zero", provider.StreamSimple, &zero, &zero, 10_000},
			{"clamped-model-default", provider.StreamSimple, nil, &clamped, 5000},
			{"direct-omits-default", provider.Stream, nil, nil, 10_000},
		} {
			t.Run(string(api)+"/"+tc.name, func(t *testing.T) {
				var requests []googleContinuationRequestV160
				model := googleContinuationModelV160(api)
				model.ContextWindow = tc.contextWindow
				opts := &goai.StreamOptions{APIKey: "test-key", MaxTokens: tc.provided, HTTPTransport: googleContinuationTransportV160(&requests)}
				checkGoogleContinuationEventsV160(t, api, tc.stream(t.Context(), model, googleContinuationContextV160(), opts))
				if opts.MaxTokens != tc.provided {
					t.Error("caller MaxTokens was changed")
				}
				if len(requests) != 1 {
					t.Fatalf("requests = %d, want 1", len(requests))
				}
				if want := googleContinuationBodyV160(tc.want); !reflect.DeepEqual(requests[0].body, want) {
					t.Errorf("token policy body = %#v, want %#v", requests[0].body, want)
				}
			})
		}
	}
}
