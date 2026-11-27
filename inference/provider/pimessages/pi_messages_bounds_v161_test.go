package pimessages

import (
	"io"
	"net/http"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161PiMessagesContentIndexes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames []string
		valid  bool
	}{
		{"large-forward-index", []string{`{"type":"text_start","contentIndex":4294967000}`}, false},
		{"invalid-index-type", []string{`{"type":"text_start","contentIndex":"__proto__"}`}, false},
		{"invalid-end-index", []string{`{"type":"text_start","contentIndex":0}`, `{"type":"toolcall_end","contentIndex":"__proto__","toolCall":{"polluted":true}}`}, false},
		{"missing-block", []string{`{"type":"text_delta","contentIndex":0,"delta":"x"}`}, false},
		{"small-gap", []string{`{"type":"text_start","contentIndex":2}`}, false},
		{"append-and-reuse", []string{`{"type":"text_start","contentIndex":0}`, `{"type":"text_start","contentIndex":0}`, `{"type":"text_delta","contentIndex":0,"delta":"x"}`, `{"type":"text_end","contentIndex":0,"content":"x"}`}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := append([]string{`{"type":"start"}`}, tc.frames...)
			frames = append(frames, `{"type":"done","reason":"stop","usage":{"input":10,"output":5,"totalTokens":15}}`)
			wire := "data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"
			transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
			})
			model := &goai.Model{ID: "auto", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "https://fixture.invalid/v1"}
			var result *goai.Message
			for event := range goai.GetApiProvider(model.Api).Stream(t.Context(), model, &goai.Context{}, &goai.StreamOptions{APIKey: "test-key", HTTPTransport: transport}) {
				switch terminal := event.(type) {
				case *goai.DoneEvent:
					result = terminal.Message
				case *goai.ErrorEvent:
					result = terminal.Error
				}
			}
			if result == nil {
				t.Fatal("missing terminal message")
			}
			if tc.valid {
				if result.StopReason != goai.StopReasonStop || len(result.Content) != 1 || goai.GetTextContent(result) != "x" {
					t.Fatalf("valid reuse: %#v", result)
				}
				return
			}
			if result.StopReason != goai.StopReasonError {
				t.Errorf("stop reason = %q; want error", result.StopReason)
			}
			if !strings.Contains(result.ErrorMessage, "Invalid contentIndex") {
				t.Errorf("diagnostic = %q; want Invalid contentIndex", result.ErrorMessage)
			}
			if len(result.Content) > 1 {
				t.Errorf("invalid input grew content to %d blocks", len(result.Content))
			}
		})
	}
}
