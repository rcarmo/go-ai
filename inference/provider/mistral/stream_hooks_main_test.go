package mistral

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMistralProviderEventHookMain(t *testing.T) {
	const wire = "data: {\"id\":\"response\",\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":0,\"total_tokens\":3},\"extra\":\"usage\"}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}],\"extra\":\"content\"}\n\ndata: [DONE]\n\n"
	for _, stop := range []bool{false, true} {
		t.Run(fmt.Sprint(stop), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, wire)
			}))
			defer server.Close()
			model := &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, BaseURL: server.URL + "/v1", Input: []string{"text"}}
			sentinel := errors.New("observer stopped")
			var seen []string
			opts := &goai.StreamOptions{APIKey: "test", OnProviderStreamEvent: func(value any, gotModel *goai.Model) error {
				if gotModel != model {
					return errors.New("wrong model")
				}
				event, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("event type %T", value)
				}
				seen = append(seen, event["extra"].(string))
				if stop && len(seen) == 2 {
					return sentinel
				}
				return nil
			}}
			done, failed := 0, 0
			for event := range streamMistral(context.Background(), model, &goai.Context{}, opts) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done++
					if e.Message.ResponseID != "response" || e.Message.Usage.Input != 3 || len(e.Message.Content) != 1 || e.Message.Content[0].Text != "answer" {
						t.Errorf("wrong result: %#v", e.Message)
					}
				case *goai.ErrorEvent:
					failed++
					if !stop || !errors.Is(e.Err, sentinel) || e.Error.ResponseID != "response" || e.Error.Usage.Input != 3 || len(e.Error.Content) != 0 {
						t.Errorf("hook failure did not preserve prior state: %#v", e)
					}
				}
			}
			if !reflect.DeepEqual(seen, []string{"usage", "content"}) {
				t.Fatal("raw hook ordering", seen)
			}
			if stop {
				if failed != 1 || done != 0 {
					t.Fatal("hook error terminal events", failed, done)
				}
			} else if failed != 0 || done != 1 {
				t.Fatal("success terminal events", failed, done)
			}
		})
	}
}

func TestMistralTerminalReasonSourceMain(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		reason  goai.StopReason
		message string
	}{
		{`"stop"`, goai.StopReasonStop, ""},
		{`"length"`, goai.StopReasonLength, ""},
		{`"model_length"`, goai.StopReasonLength, ""},
		{`"tool_calls"`, goai.StopReasonToolUse, ""},
		{`"error"`, goai.StopReasonError, "Provider stopped with: error (server error)"},
		{`"unmapped_error"`, goai.StopReasonError, "Provider stopped with: unmapped_error"},
		{`""`, goai.StopReasonError, "Mistral stream ended without a finish reason"},
		{`null`, goai.StopReasonError, "Mistral stream ended without a finish reason"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			done, failed := 0, 0
			wire := "data: {\"choices\":[{\"finish_reason\":" + tc.raw + ",\"delta\":{\"content\":\"partial\"}}]}\n\ndata: [DONE]\n\n"
			processSSEStream(strings.NewReader(wire), &goai.Model{}, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done++
					if e.Reason != tc.reason {
						t.Errorf("reason %s", e.Reason)
					}
				case *goai.ErrorEvent:
					failed++
					if tc.reason != goai.StopReasonError || e.Reason != tc.reason || e.Error.ErrorMessage != tc.message || len(e.Error.Content) != 1 || e.Error.Content[0].Text != "partial" {
						t.Errorf("error result %#v", e)
					}
				}
			})
			if tc.reason == goai.StopReasonError {
				if failed != 1 || done != 0 {
					t.Fatal("error must not emit done", failed, done)
				}
			} else if failed != 0 || done != 1 {
				t.Fatal("normal terminal events", failed, done)
			}
		})
	}
}

func TestMistralProviderHookCancellationMain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, failed, ends := 0, 0, 0
	opts := &goai.StreamOptions{OnProviderStreamEvent: func(any, *goai.Model) error { cancel(); return nil }}
	processSSEStreamWithOptions(ctx, strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"stop\"}]}\n\n"), &goai.Model{}, opts, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			done++
		case *goai.TextEndEvent:
			ends++
		case *goai.ErrorEvent:
			failed++
			if e.Reason != goai.StopReasonAborted || !errors.Is(e.Err, context.Canceled) || len(e.Error.Content) != 1 || e.Error.Content[0].Text != "partial" {
				t.Errorf("canceled hook result: %#v", e)
			}
		}
	})
	if failed != 1 || done != 0 || ends != 0 {
		t.Fatal("canceled stream completed", failed, done, ends)
	}
}
