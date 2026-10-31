package mistral

// Port of packages/ai/test/mistral-http-transport.test.ts at 4b535d57.
// Fetch/ReadableStream become local HTTP servers/readers. Go header names are
// canonicalised; the callback exposes a typed wire payload. The first case's
// provider-only options use the payload hook because StreamOptions has no
// promptMode/reasoningEffort/named-tool fields. That API coverage is partial.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func upstreamMistralHTTP(t *testing.T, handler http.HandlerFunc) *goai.Model {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	model := upstreamMistralModel("mistral-large-latest")
	model.BaseURL = server.URL + "/v1"
	model.Input = []string{"text", "image"}
	return model
}
func upstreamMistralTerminal() map[string]any {
	return map[string]any{"id": "mistral-response-id", "model": "mistral-large-latest", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "delta": map[string]any{}}}, "usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}
}
func upstreamMistralWrite(w http.ResponseWriter, events ...any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(w, "data: %s\r\n\r\n", raw)
	}
	fmt.Fprint(w, "data: [DONE]\r\n\r\n")
}
func upstreamAssertJSON(t *testing.T, got any, want string) {
	t.Helper()
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err = json.Unmarshal(raw, &actual); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %s\nwant %s", raw, want)
	}
}

func TestUpstreamMainMistralHTTPTransport(t *testing.T) {
	t.Run("serializes SDK-style payloads to wire format (partial provider-option API)", func(t *testing.T) {
		var headers http.Header
		var wire, callback map[string]any
		var path string
		var responseStatus int
		var responseHeaders map[string]string
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			headers = r.Header.Clone()
			path = r.URL.Path
			if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
				t.Error(err)
			}
			w.Header()["Date"] = nil
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("x-request-id", "request-1")
			w.(http.Flusher).Flush()
			upstreamMistralWrite(w, upstreamMistralTerminal())
		})
		transcript := &goai.Context{SystemPrompt: "Be precise", Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "describe"}, {Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}}}, Tools: []goai.Tool{{Name: "lookup", Description: "Look something up", Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)}}}
		max := 123
		message := upstreamMistralResult(t, context.Background(), model, transcript, &goai.StreamOptions{APIKey: "secret", Headers: map[string]string{"x-custom": "value"}, MaxTokens: &max, SessionID: "session-1", OnPayload: func(value any, _ *goai.Model) (any, error) {
			var err error
			callback, err = upstreamMistralPayload(value)
			if err != nil {
				return nil, err
			}
			next := make(map[string]any, len(callback))
			for key, value := range callback {
				next[key] = value
			}
			next["promptMode"] = "reasoning"
			next["reasoningEffort"] = "high"
			next["toolChoice"] = map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}
			next["topP"] = 0.9
			next["randomSeed"] = 42
			next["responseFormat"] = map[string]any{"type": "json_schema", "jsonSchema": map[string]any{"name": "result", "schemaDefinition": map[string]any{"type": "object", "properties": map[string]any{"maxTokens": map[string]any{"type": "number"}}}}}
			next["presencePenalty"] = 0.1
			next["frequencyPenalty"] = 0.2
			next["parallelToolCalls"] = true
			next["safePrompt"] = true
			return next, nil
		}, OnResponse: func(status int, raw map[string]string, _ *goai.Model) {
			responseStatus = status
			responseHeaders = map[string]string{}
			for key, value := range raw {
				responseHeaders[strings.ToLower(key)] = value
			}
		}})
		if message.StopReason != goai.StopReasonStop || path != "/v1/chat/completions" {
			t.Fatal(message, path)
		}
		for key, want := range map[string]string{"authorization": "Bearer secret", "accept": "text/event-stream", "x-affinity": "session-1", "x-custom": "value", "user-agent": goai.FormatUserAgent(nil)} {
			if headers.Get(key) != want {
				t.Errorf("%s=%q want %q", key, headers.Get(key), want)
			}
		}
		if callback["max_tokens"] != float64(123) || callback["prompt_cache_key"] != "session-1" {
			t.Fatal("callback payload", callback)
		}
		if responseStatus != 200 || !reflect.DeepEqual(responseHeaders, map[string]string{"content-type": "text/event-stream", "x-request-id": "request-1"}) {
			t.Fatal("response callback", responseStatus, responseHeaders)
		}
		for key, want := range map[string]any{"max_tokens": float64(123), "prompt_mode": "reasoning", "reasoning_effort": "high", "prompt_cache_key": "session-1", "top_p": 0.9, "random_seed": float64(42), "presence_penalty": 0.1, "frequency_penalty": 0.2, "parallel_tool_calls": true, "safe_prompt": true} {
			if wire[key] != want {
				t.Errorf("%s=%v want %v", key, wire[key], want)
			}
		}
		upstreamAssertJSON(t, wire["tool_choice"], `{"type":"function","function":{"name":"lookup"}}`)
		upstreamAssertJSON(t, wire["response_format"], `{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object","properties":{"maxTokens":{"type":"number"}}}}}`)
		for _, key := range []string{"maxTokens", "promptMode", "promptCacheKey"} {
			if _, ok := wire[key]; ok {
				t.Error("SDK name leaked", key)
			}
		}
		upstreamAssertJSON(t, wire["messages"], `[{"role":"system","content":"Be precise"},{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":"data:image/png;base64,aGVsbG8="}]}]`)
	})
	t.Run("serializes assistant thinking tool calls and results for replay", func(t *testing.T) {
		var wire map[string]any
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
				t.Error(err)
			}
			upstreamMistralWrite(w, upstreamMistralTerminal())
		})
		transcript := &goai.Context{Messages: []goai.Message{{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "reason"}, {Type: "text", Text: "answer"}, {Type: "toolCall", ID: "abc123456", Name: "lookup", Arguments: map[string]any{"query": "pi"}}}, Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse, Timestamp: 1}, {Role: goai.RoleToolResult, ToolCallID: "abc123456", ToolName: "lookup", Content: []goai.ContentBlock{{Type: "text", Text: "found"}, {Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}, Timestamp: 2}}}
		message := upstreamMistralResult(t, context.Background(), model, transcript, &goai.StreamOptions{APIKey: "test"})
		if message.StopReason != goai.StopReasonStop {
			t.Fatal(message)
		}
		upstreamAssertJSON(t, wire["messages"], `[{"role":"assistant","prefix":false,"content":[{"type":"thinking","thinking":[{"type":"text","text":"reason"}]},{"type":"text","text":"answer"}],"tool_calls":[{"id":"abc123456","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"pi\"}"},"index":0}]},{"role":"tool","tool_call_id":"abc123456","name":"lookup","content":[{"type":"text","text":"found"},{"type":"image_url","image_url":"data:image/png;base64,aGVsbG8="}]}]`)
	})
	t.Run("parses native thinking text fragmented calls and cached usage", func(t *testing.T) {
		frames := []string{`{"id":"response-1","model":"mistral-large-latest","choices":[{"index":0,"finish_reason":null,"delta":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"reason"}]}]}}]}`, `{"id":"response-1","model":"mistral-large-latest","choices":[{"index":0,"finish_reason":null,"delta":{"content":[{"type":"text","text":"answer"}]}}]}`, `{"id":"response-1","model":"mistral-large-latest","choices":[{"index":0,"finish_reason":null,"delta":{"tool_calls":[{"id":"abc123456","index":0,"function":{"name":"lookup","arguments":"{\"query\":"}}]}}]}`, `{"id":"response-1","model":"mistral-large-latest","choices":[{"index":0,"finish_reason":"tool_calls","delta":{"tool_calls":[{"index":0,"function":{"name":"","arguments":"\"pi\"}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":3}}}`}
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			events := make([]any, len(frames))
			for i, frame := range frames {
				events[i] = json.RawMessage(frame)
			}
			upstreamMistralWrite(w, events...)
		})
		message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test"})
		if message.StopReason != goai.StopReasonToolUse || message.RawStopReason != "tool_calls" || message.ResponseID != "response-1" {
			t.Fatal(message)
		}
		upstreamAssertJSON(t, message.Content, `[{"type":"thinking","thinking":"reason"},{"type":"text","text":"answer"},{"type":"toolCall","id":"abc123456","name":"lookup","arguments":{"query":"pi"}}]`)
		usage := message.Usage
		if usage == nil || usage.Input != 7 || usage.Output != 4 || usage.CacheRead != 3 || usage.CacheWrite != 0 || usage.TotalTokens != 14 {
			t.Fatal("usage", usage)
		}
	})
	t.Run("ignores empty content deltas", func(t *testing.T) {
		deltas := []json.RawMessage{json.RawMessage(`{"content":""}`), json.RawMessage(`{"content":[{"type":"thinking","thinking":[{"type":"text","text":"first part,"}]}]}`), json.RawMessage(`{"content":""}`), json.RawMessage(`{"content":[{"type":"text","text":""}]}`), json.RawMessage(`{"content":[{"type":"thinking","thinking":[{"type":"text","text":" second part."}]},{"type":"text","text":"Reading."}]}`), json.RawMessage(`{"content":"","tool_calls":[{"index":0,"id":"abc123456","function":{"name":"read","arguments":""}}]}`), json.RawMessage(`{"content":"","tool_calls":[{"index":0,"function":{"name":"","arguments":"{\"path\":"}}]}`), json.RawMessage(`{"content":"","tool_calls":[{"index":0,"function":{"name":"","arguments":"\"a.txt\"}"}}]}`), json.RawMessage(`{"content":""}`)}
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			events := make([]any, len(deltas))
			for i, delta := range deltas {
				var finish any
				if i == len(deltas)-1 {
					finish = "tool_calls"
				}
				events[i] = map[string]any{"id": "response-1", "model": "zai-glm-5-3", "choices": []any{map[string]any{"index": 0, "finish_reason": finish, "delta": delta}}}
			}
			upstreamMistralWrite(w, events...)
		})
		model.ID = "zai-glm-5-3"
		message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test"})
		upstreamAssertJSON(t, message.Content, `[{"type":"thinking","thinking":"first part, second part."},{"type":"text","text":"Reading."},{"type":"toolCall","id":"abc123456","name":"read","arguments":{"path":"a.txt"}}]`)
	})
	t.Run("forwards each parsed SSE payload before normalization", func(t *testing.T) {
		frames := []json.RawMessage{json.RawMessage(`{"id":"response-1","provider_metadata":{"request":"test"},"choices":[{"index":0,"finish_reason":null,"delta":{"content":"hello"}}]}`), json.RawMessage(`{"id":"response-1","choices":[{"index":0,"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)}
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) { upstreamMistralWrite(w, frames[0], frames[1]) })
		var received []any
		var models []*goai.Model
		message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test", OnProviderStreamEvent: func(event any, m *goai.Model) error {
			received = append(received, event)
			models = append(models, m)
			return nil
		}})
		upstreamAssertJSON(t, received, "["+string(frames[0])+","+string(frames[1])+"]")
		if !reflect.DeepEqual(models, []*goai.Model{model, model}) || message.StopReason != goai.StopReasonStop {
			t.Fatal(models, message)
		}
		upstreamAssertJSON(t, message.Content, `[{"type":"text","text":"hello"}]`)
	})
	t.Run("parses SSE and UTF-8 split across chunks", TestV0842MistralRawByteSplitUTF8Stream)
	t.Run("honors case-insensitive overrides and affinity suppression", func(t *testing.T) {
		var got http.Header
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Clone()
			upstreamMistralWrite(w, upstreamMistralTerminal())
		})
		model.Headers = map[string]string{"Authorization": "Bearer model-key", "X-Affinity": "model-affinity"}
		upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "request-key", SessionID: "automatic-affinity", Headers: map[string]string{"User-Agent": "custom-agent"}, SuppressHeaders: []string{"authorization", "x-affinity"}})
		if _, ok := got["Authorization"]; ok {
			t.Fatal("authorization not suppressed", got)
		}
		if _, ok := got["X-Affinity"]; ok {
			t.Fatal("affinity not suppressed", got)
		}
		if got.Get("User-Agent") != "custom-agent" {
			t.Fatal(got)
		}
	})
	t.Run("aborts while waiting for an SSE chunk", func(t *testing.T) {
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var result *goai.Message
		for event := range streamMistral(ctx, model, makeMistralContext(), &goai.StreamOptions{APIKey: "test"}) {
			switch e := event.(type) {
			case *goai.StartEvent:
				cancel()
			case *goai.ErrorEvent:
				result = e.Error
				if e.Reason != goai.StopReasonAborted {
					t.Errorf("reason %s", e.Reason)
				}
			case *goai.DoneEvent:
				t.Error("canceled stream completed")
			}
		}
		if result == nil || result.StopReason != goai.StopReasonAborted {
			t.Fatal(result)
		}
	})
	t.Run("times out while waiting for response headers", func(t *testing.T) {
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			// Drain the Go request body so net/http can observe disconnects.
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			<-r.Context().Done()
		})
		timeout := 5
		message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test", TimeoutMs: &timeout})
		if message.StopReason != goai.StopReasonError || message.ErrorMessage != "Mistral response headers timed out after 5ms" {
			t.Fatal(message)
		}
	})
	t.Run("does not abort an active stream after request timeout", func(t *testing.T) {
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			for i := 0; i < 5; i++ {
				writeMistralSSEForTest(w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": []any{map[string]any{"type": "thinking", "thinking": []any{map[string]any{"type": "text", "text": "x"}}}}}}}})
				w.(http.Flusher).Flush()
				time.Sleep(10 * time.Millisecond)
			}
			upstreamMistralWrite(w, upstreamMistralTerminal())
		})
		timeout := 20
		message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test", TimeoutMs: &timeout})
		if message.StopReason != goai.StopReasonStop {
			t.Fatal(message)
		}
		upstreamAssertJSON(t, message.Content, `[{"type":"thinking","thinking":"xxxxx"}]`)
	})
	t.Run("preserves HTTP status and response body in errors", func(t *testing.T) {
		model := upstreamMistralHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"blocked by gateway"}`)
		})
		message := upstreamMistralResult(t, context.Background(), model, makeMistralContext(), &goai.StreamOptions{APIKey: "test"})
		if message.StopReason != goai.StopReasonError || message.ErrorMessage != `Mistral API error (403): {"message":"blocked by gateway"}` {
			t.Fatal(message)
		}
	})
}
