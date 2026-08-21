package mistral

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func writeMistralSSEForTest(w http.ResponseWriter, payload any) {
	data, _ := json.Marshal(payload)
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n\n"))
}

func mistralToolCallChunk(id, name, arguments string) map[string]any {
	fn := map[string]any{"arguments": arguments}
	if name != "" {
		fn["name"] = name
	}
	call := map[string]any{"index": 0, "function": fn}
	if id != "" {
		call["id"] = id
	}
	return map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{call}}}}}
}

func collectMistralDone(t *testing.T, model *goai.Model, ctx *goai.Context, opts *goai.StreamOptions) *goai.Message {
	t.Helper()
	var done *goai.Message
	for ev := range streamMistral(context.Background(), model, ctx, opts) {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			done = e.Message
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if done == nil {
		t.Fatal("missing done event")
	}
	return done
}

func TestV0842MistralWireContractHeadersReplayUsageAndUTF8(t *testing.T) {
	var capturedHeader http.Header
	var capturedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&capturedPayload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hé"))
		_, _ = w.Write([]byte("llo 🌍\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\n"))
		writeMistralSSEForTest(w, mistralToolCallChunk("tool12345", "lookup", "{\"q\":\""))
		writeMistralSSEForTest(w, mistralToolCallChunk("", "", "pi\"}"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"tool_calls\",\"delta\":{}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, BaseURL: server.URL, Cost: goai.ModelCost{Input: 1, Output: 2, CacheRead: 0.5}}
	ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "reason"}, {Type: "text", Text: "answer"}, {Type: "toolCall", ID: "abc-123456", Name: "lookup", Arguments: map[string]any{"q": "pi"}}}, Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonToolUse, Usage: &goai.Usage{}}, {Role: goai.RoleToolResult, ToolCallID: "abc-123456", ToolName: "lookup", Content: []goai.ContentBlock{{Type: "text", Text: "found"}}}, goai.UserMessage("next")}}
	done := collectMistralDone(t, model, ctx, &goai.StreamOptions{APIKey: "secret", SessionID: "session-1"})

	if got := capturedHeader.Get("x-affinity"); got != "session-1" {
		t.Fatalf("x-affinity=%q, want session-1", got)
	}
	if got := capturedHeader.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("authorization=%q", got)
	}
	if capturedPayload["prompt_cache_key"] != "session-1" {
		t.Fatalf("prompt_cache_key missing: %#v", capturedPayload)
	}
	messages, _ := capturedPayload["messages"].([]any)
	if len(messages) < 3 {
		t.Fatalf("expected replayed assistant/tool/user messages, got %#v", capturedPayload["messages"])
	}
	assistant := messages[0].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Fatalf("first replay message=%#v", assistant)
	}
	toolCalls := assistant["tool_calls"].([]any)
	call := toolCalls[0].(map[string]any)
	if call["type"] != "function" || call["id"] == "abc-123456" {
		t.Fatalf("tool call not normalized/replayed: %#v", call)
	}
	if done.StopReason != goai.StopReasonToolUse || goai.GetTextContent(done) != "héllo 🌍" {
		t.Fatalf("unexpected done: %#v text=%q", done, goai.GetTextContent(done))
	}
	if done.Usage == nil || done.Usage.Input != 6 || done.Usage.CacheRead != 4 || done.Usage.Output != 2 || done.Usage.TotalTokens != 12 {
		t.Fatalf("usage cache-read mapping wrong: %#v", done.Usage)
	}
	calls := goai.GetToolCalls(done)
	if len(calls) != 1 || calls[0].ID != "tool12345" || calls[0].Name != "lookup" || calls[0].Arguments["q"] != "pi" {
		t.Fatalf("streamed tool call wrong: %#v", calls)
	}
}

func TestV0842MistralAffinityHeaderSuppressionAndOverride(t *testing.T) {
	cases := []struct {
		name         string
		modelHeaders map[string]string
		optsHeaders  map[string]string
		suppress     []string
		wantAffinity string
		wantPresent  bool
	}{
		{name: "default", wantAffinity: "session-1", wantPresent: true},
		{name: "caller override", optsHeaders: map[string]string{"X-Affinity": "caller"}, wantAffinity: "caller", wantPresent: true},
		{name: "model override", modelHeaders: map[string]string{"X-Affinity": "model"}, wantAffinity: "model", wantPresent: true},
		{name: "case insensitive suppress", suppress: []string{"X-AFFINITY"}, wantPresent: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{}}]}\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer server.Close()
			model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, BaseURL: server.URL, Headers: tc.modelHeaders}
			_ = collectMistralDone(t, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "secret", SessionID: "session-1", Headers: tc.optsHeaders, SuppressHeaders: tc.suppress})
			_, present := got[http.CanonicalHeaderKey("x-affinity")]
			if present != tc.wantPresent {
				t.Fatalf("presence=%v, want %v headers=%#v", present, tc.wantPresent, got)
			}
			if tc.wantPresent && got.Get("x-affinity") != tc.wantAffinity {
				t.Fatalf("x-affinity=%q, want %q", got.Get("x-affinity"), tc.wantAffinity)
			}
		})
	}
}

func TestV0842MistralRawByteSplitUTF8Stream(t *testing.T) {
	payload := "data: {\"choices\":[{\"delta\":{\"content\":\"héllo 🌍\"}}]}\n\n" +
		"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{}}]}\n\n" +
		"data: [DONE]\n\n"
	ch := make(chan goai.Event, 16)
	processSSEStream(&bytewiseReaderForMistralTest{data: []byte(payload)}, &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, ch)
	close(ch)
	var done *goai.Message
	for ev := range ch {
		if e, ok := ev.(*goai.DoneEvent); ok {
			done = e.Message
		}
	}
	if done == nil || goai.GetTextContent(done) != "héllo 🌍" {
		t.Fatalf("byte-split UTF-8 text not preserved: %#v", done)
	}
}

func TestV0842MistralAbortAndTimeoutWhileAwaitingChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, BaseURL: server.URL}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sawAbort bool
	for ev := range streamMistral(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "secret"}) {
		switch e := ev.(type) {
		case *goai.StartEvent:
			cancel()
		case *goai.ErrorEvent:
			if e.Reason == goai.StopReasonAborted || e.Reason == goai.StopReasonError {
				sawAbort = true
			}
		}
	}
	if !sawAbort {
		t.Fatal("expected aborted/error event after cancelling while awaiting chunks")
	}

	var sawTimeout bool
	for ev := range streamMistral(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "secret", TimeoutMs: intPtrForMistralTest(25)}) {
		if e, ok := ev.(*goai.ErrorEvent); ok && e.Reason == goai.StopReasonError {
			sawTimeout = true
		}
	}
	if !sawTimeout {
		t.Fatal("expected timeout error while awaiting chunks")
	}
}

func TestV0842MistralBounded403JSONBodyAndReplayableRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"retry"}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"` + strings.Repeat("x", 5000) + `"}`))
	}))
	defer server.Close()
	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, BaseURL: server.URL}
	opts := &goai.StreamOptions{APIKey: "secret", RetryConfig: &goai.RetryConfig{MaxRetries: 1, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, BackoffMultiplier: 1, ConnectTimeout: time.Second, RequestTimeout: time.Second}}
	var gotErr error
	for ev := range streamMistral(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			gotErr = e.Err
		}
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected replayable retry attempts=2, got %d", attempts.Load())
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "HTTP 403") {
		t.Fatalf("expected bounded 403 error, got %v", gotErr)
	}
	if len(gotErr.Error()) > 4200 {
		t.Fatalf("403 body was not bounded, len=%d", len(gotErr.Error()))
	}
}

type bytewiseReaderForMistralTest struct {
	data []byte
	pos  int
}

func (r *bytewiseReaderForMistralTest) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	p[0] = r.data[r.pos]
	r.pos++
	return 1, nil
}

func intPtrForMistralTest(v int) *int { return &v }
