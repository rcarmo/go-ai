package pimessages

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestPiMessagesPostsMessagesDebugHeadersAndConvertsSSE(t *testing.T) {
	var gotPath, gotDebug, gotAuth, gotCustom string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotDebug = r.URL.Query().Get("debug")
		gotAuth = r.Header.Get("authorization")
		gotCustom = r.Header.Get("x-test")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"text_start\",\"contentIndex\":0}\n\n" +
			"data: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"hel\"}\n\n" +
			"data: {\"type\":\"text_end\",\"contentIndex\":0,\"content\":\"hello\"}\n\n" +
			"data: {\"type\":\"thinking_start\",\"contentIndex\":1}\n\n" +
			"data: {\"type\":\"thinking_delta\",\"contentIndex\":1,\"delta\":\"hmm\"}\n\n" +
			"data: {\"type\":\"thinking_end\",\"contentIndex\":1,\"content\":\"hmm\",\"redacted\":true}\n\n" +
			"data: {\"type\":\"toolcall_start\",\"contentIndex\":2,\"id\":\"call_1\",\"toolName\":\"run\"}\n\n" +
			"data: {\"type\":\"toolcall_delta\",\"contentIndex\":2,\"delta\":\"{\\\"x\\\":1}\"}\n\n" +
			"data: {\"type\":\"toolcall_end\",\"contentIndex\":2,\"toolCall\":{\"type\":\"toolCall\",\"id\":\"call_1\",\"name\":\"run\",\"arguments\":{\"x\":1}}}\n\n" +
			"data: {\"type\":\"done\",\"reason\":\"toolUse\",\"responseId\":\"resp_1\",\"usage\":{\"input\":2,\"output\":3,\"totalTokens\":5},\"rewrite\":{\"why\":\"normalized\"}}\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "pi-test", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: server.URL}
	opts := &goai.StreamOptions{APIKey: "secret", Headers: map[string]string{"x-test": "yes"}, Metadata: map[string]any{"debug": true}}
	var done *goai.DoneEvent
	for ev := range stream(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
		if d, ok := ev.(*goai.DoneEvent); ok {
			done = d
		}
	}
	if gotPath != "/messages" || gotDebug != "1" || gotAuth != "Bearer secret" || gotCustom != "yes" {
		t.Fatalf("bad request path=%s debug=%s auth=%s custom=%s", gotPath, gotDebug, gotAuth, gotCustom)
	}
	if gotPayload["model"] != "pi-test" {
		t.Fatalf("payload model=%v", gotPayload["model"])
	}
	if done == nil || done.Message.ResponseID != "resp_1" || done.Reason != goai.StopReasonToolUse {
		t.Fatalf("missing done %#v", done)
	}
	if done.Message.Content[0].Text != "hello" || !done.Message.Content[1].Redacted || done.Message.Content[2].Name != "run" {
		t.Fatalf("bad content %#v", done.Message.Content)
	}
	if len(done.Message.Diagnostics) != 1 || done.Message.Diagnostics[0].Type != "pi_messages_rewrite" {
		t.Fatalf("missing rewrite diagnostic %#v", done.Message.Diagnostics)
	}
}

func TestPiMessagesErrors(t *testing.T) {
	model := &goai.Model{ID: "pi-test", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: "http://example.invalid"}
	ev := <-stream(context.Background(), model, &goai.Context{}, nil)
	if e, ok := ev.(*goai.ErrorEvent); !ok || e.Err == nil || e.Error == nil || e.Error.StopReason != goai.StopReasonError {
		t.Fatalf("missing-key error %#v", ev)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"text_start\",\"contentIndex\":0}\n\n"))
	}))
	defer server.Close()
	model.BaseURL = server.URL
	ev = nil
	for x := range stream(context.Background(), model, &goai.Context{}, &goai.StreamOptions{APIKey: "secret"}) {
		ev = x
	}
	if e, ok := ev.(*goai.ErrorEvent); !ok || e.Err == nil || e.Error.ErrorMessage != "radius stream ended without a terminal event" {
		t.Fatalf("terminal error %#v", ev)
	}
}

func TestPiMessagesHTTPErrorDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","code":"rate_limit"}}`))
	}))
	defer server.Close()
	model := &goai.Model{ID: "pi-test", Provider: "radius", Api: goai.ApiPiMessages, BaseURL: server.URL}
	ev := <-stream(context.Background(), model, &goai.Context{}, &goai.StreamOptions{APIKey: "secret"})
	e, ok := ev.(*goai.ErrorEvent)
	if !ok || e.Error == nil || len(e.Error.Diagnostics) != 1 || e.Error.Diagnostics[0].Type != "pi_messages_response_failure" {
		t.Fatalf("diagnostic %#v", ev)
	}
}
