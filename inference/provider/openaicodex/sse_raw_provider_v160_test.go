package openaicodex

import (
	"encoding/base64"
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

// Source: pi v1.6.0 openai-codex-stream.test.ts:157–284. This fixture
// covers the Codex SSE request and callbacks, not GitHub prompt bootstrap.
func TestSourceV160CodexSSEForwardsRawProviderEvents(t *testing.T) {
	frames := []string{
		`{"type":"response.output_item.added","item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
		`{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`,
		`{"type":"response.output_text.delta","delta":"Hello"}`,
		`{"type":"response.output_item.done","item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello"}]}}`,
		`{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8,"input_tokens_details":{"cached_tokens":0}}}}`,
	}
	var wire strings.Builder
	wantRaw := make([]map[string]any, len(frames))
	for i, frame := range frames {
		fmt.Fprintf(&wire, "data: %s\n\n", frame)
		if err := json.Unmarshal([]byte(frame), &wantRaw[i]); err != nil {
			t.Fatal(err)
		}
	}
	token := "aaa." + base64.StdEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acc_test"}}`)) + ".bbb"
	requests := 0
	var headers http.Header
	transport := codexTerminalTransportV160(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		requests++
		if requests != 1 || r.Method != http.MethodPost || r.URL.String() != "https://chatgpt.com/backend-api/codex/responses" {
			return nil, fmt.Errorf("unexpected Codex scripted request %d: %s %s", requests, r.Method, r.URL)
		}
		headers = r.Header.Clone()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(wire.String()))}, nil
	})
	model := &goai.Model{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
		BaseURL: "https://chatgpt.com/backend-api", Reasoning: true, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000}
	transcript := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Say hello")}}
	var rawEvents []map[string]any
	var eventModels []*goai.Model
	var events []string
	var terminal *goai.DoneEvent
	zero := 0
	for event := range goai.Stream(t.Context(), model, transcript, &goai.StreamOptions{APIKey: token, MaxRetries: &zero,
		Transport: goai.TransportSSE, HTTPTransport: transport, OnProviderStreamEvent: func(value any, eventModel *goai.Model) error {
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			var raw map[string]any
			if err := json.Unmarshal(encoded, &raw); err != nil {
				return err
			}
			rawEvents = append(rawEvents, raw)
			eventModels = append(eventModels, eventModel)
			return nil
		}}) {
		switch e := event.(type) {
		case *goai.StartEvent:
			events = append(events, "start")
		case *goai.TextStartEvent:
			events = append(events, fmt.Sprintf("text_start:%d", e.ContentIndex))
		case *goai.TextDeltaEvent:
			events = append(events, fmt.Sprintf("text_delta:%d:%s", e.ContentIndex, e.Delta))
		case *goai.TextEndEvent:
			events = append(events, fmt.Sprintf("text_end:%d:%s", e.ContentIndex, e.Content))
		case *goai.DoneEvent:
			events = append(events, "done")
			terminal = e
		case *goai.ErrorEvent:
			t.Errorf("public Codex SSE error: %v", e.Err)
		default:
			t.Errorf("unexpected public event %T", event)
		}
	}
	if requests != 1 {
		t.Errorf("scripted request count = %d, want 1", requests)
	}
	for _, want := range []struct{ key, value string }{
		{"Authorization", "Bearer " + token}, {"ChatGPT-Account-Id", "acc_test"}, {"OpenAI-Beta", "responses=experimental"},
		{"originator", "pi"}, {"User-Agent", goai.FormatUserAgent(nil)}, {"Accept", "text/event-stream"},
	} {
		// The User-Agent uses native Go runtime metadata, not Node's runtime string.
		if !slices.Equal(headers.Values(want.key), []string{want.value}) {
			t.Errorf("%s header = %q, want %q", want.key, headers.Values(want.key), want.value)
		}
	}
	if len(headers.Values("X-Api-Key")) != 0 {
		t.Error("Codex SSE must omit X-Api-Key")
	}
	if !reflect.DeepEqual(rawEvents, wantRaw) {
		t.Errorf("raw callback payload/order = %#v; want %#v", rawEvents, wantRaw)
	}
	if !slices.Equal(eventModels, []*goai.Model{model, model, model, model, model}) {
		t.Errorf("raw callback model pointers = %v, want supplied model for each of 5 events", eventModels)
	}
	if !slices.Equal(events, []string{"start", "text_start:0", "text_delta:0:Hello", "text_end:0:Hello", "done"}) {
		t.Errorf("public event sequence = %v", events)
	}
	if terminal == nil || terminal.Message == nil || terminal.Reason != goai.StopReasonStop || terminal.Message.StopReason != goai.StopReasonStop {
		t.Fatalf("terminal event = %+v, want stop", terminal)
	}
	m := terminal.Message
	if len(m.Content) != 1 || m.Content[0].Type != "text" || m.Content[0].Text != "Hello" || m.ErrorMessage != "" || m.EndTurn != nil {
		t.Errorf("terminal message content/endTurn = %+v/%v; err=%q", m.Content, m.EndTurn, m.ErrorMessage)
	}
	if m.Usage == nil || m.Usage.Input != 5 || m.Usage.Output != 3 || m.Usage.TotalTokens != 8 || m.Usage.CacheRead != 0 {
		t.Errorf("terminal usage = %+v; want 5 input, 3 output, 8 total, 0 cached", m.Usage)
	}
}
