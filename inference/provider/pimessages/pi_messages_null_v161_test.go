package pimessages

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type piMessagesFixtureTransport func(*http.Request) (*http.Response, error)

func (f piMessagesFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSourceV161PiMessagesSkipsNullEvents(t *testing.T) {
	const start = `{"type":"start"}`
	const done = `{"type":"done","reason":"stop","usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":15,"cost":{"input":0.1,"output":0.2,"cacheRead":0,"cacheWrite":0,"total":0.3}},"responseId":"resp_1"}`
	var want []any
	if err := json.Unmarshal([]byte("["+start+","+done+"]"), &want); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, wire string
		reject     bool
	}{
		{"source", "data: " + start + "\n\ndata: null\n\ndata: " + done + "\n\n", false},
		{"crlf-and-terminal-tail", "data: " + start + "\r\n\r\ndata:    null \r\n\r\ndata: " + done, false},
		{"hook-refusal", "data: " + start + "\n\ndata: null\n\ndata: " + done + "\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: "pi-test", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid"}
			transport := piMessagesFixtureTransport(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				if r.Method != http.MethodPost || r.URL.Path != "/messages" || r.Header.Get("Authorization") != "Bearer test-key" {
					return nil, fmt.Errorf("unexpected fixture request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.wire))}, nil
			})
			refusal := errors.New("fixture rejected raw Pi Messages start")
			var received []any
			var starts, terminals int
			var result *goai.Message
			var terminalErr error
			for evt := range goai.GetApiProvider(model.Api).StreamSimple(t.Context(), model, &goai.Context{}, &goai.StreamOptions{
				APIKey: "test-key", HTTPTransport: transport, OnProviderStreamEvent: func(event any, callbackModel *goai.Model) error {
					if callbackModel != model {
						return errors.New("incorrect hook model")
					}
					received = append(received, event)
					if tc.reject {
						return refusal
					}
					return nil
				},
			}) {
				switch event := evt.(type) {
				case *goai.StartEvent:
					starts++
				case *goai.DoneEvent:
					terminals++
					result = event.Message
				case *goai.ErrorEvent:
					terminals++
					terminalErr = event.Err
					result = event.Error
				}
			}
			if tc.reject {
				if !reflect.DeepEqual(received, want[:1]) || starts != 0 || terminals != 1 || !errors.Is(terminalErr, refusal) {
					t.Fatalf("hook refusal: observed %#v, starts %d, terminals %d, error %v", received, starts, terminals, terminalErr)
				}
				return
			}
			if !reflect.DeepEqual(received, want) {
				t.Errorf("raw events = %#v; want %#v", received, want)
			}
			if result == nil || result.StopReason != goai.StopReasonStop || terminals != 1 || terminalErr != nil {
				t.Errorf("terminal: got %#v (%d, %v); want stop", result, terminals, terminalErr)
			}
			if starts != 1 {
				t.Errorf("native starts = %d; want one", starts)
			}
		})
	}
}
