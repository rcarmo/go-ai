package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func cacheRetentionContext() *goai.Context {
	return &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Hello")}, Tools: []goai.Tool{{Name: "read", Description: "Read", Parameters: json.RawMessage(`{"type":"object"}`)}}}
}

func cacheRetentionAnthropicModel(mut func(*goai.Model)) *goai.Model {
	m := &goai.Model{ID: "claude-haiku-4-5", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: "https://api.anthropic.com"}
	if mut != nil {
		mut(m)
	}
	return m
}

func firstSystemCacheControl(t *testing.T, req anthropicRequest) *cacheControl {
	t.Helper()
	var sys []struct {
		CacheControl *cacheControl `json:"cache_control"`
	}
	if err := json.Unmarshal(req.System, &sys); err != nil {
		t.Fatal(err)
	}
	if len(sys) == 0 {
		t.Fatal("missing system block")
	}
	return sys[0].CacheControl
}

func TestCacheRetentionAnthropicDefaultNoTTL(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), nil)
	cc := firstSystemCacheControl(t, req)
	if cc == nil || cc.Type != "ephemeral" || cc.TTL != "" {
		t.Fatalf("cache_control=%#v", cc)
	}
}

func TestCacheRetentionAnthropicLongUsesOneHourTTL(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionLong})
	cc := firstSystemCacheControl(t, req)
	if cc == nil || cc.Type != "ephemeral" || cc.TTL != "1h" {
		t.Fatalf("cache_control=%#v", cc)
	}
}

func TestCacheRetentionAnthropicOmitTTLWhenLongUnsupported(t *testing.T) {
	noLong := false
	req := buildRequest(cacheRetentionAnthropicModel(func(m *goai.Model) {
		m.AnthropicCompat = &goai.AnthropicMessagesCompat{SupportsLongCacheRetention: &noLong}
	}), cacheRetentionContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionLong})
	cc := firstSystemCacheControl(t, req)
	if cc == nil || cc.Type != "ephemeral" || cc.TTL != "" {
		t.Fatalf("cache_control=%#v", cc)
	}
}

func TestCacheRetentionAnthropicNoneOmitsCacheControl(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})
	if cc := firstSystemCacheControl(t, req); cc != nil {
		t.Fatalf("cache_control=%#v, want nil", cc)
	}
}

func TestCacheRetentionAnthropicAddsCacheControlToLastUserMessageAndTool(t *testing.T) {
	req := buildRequest(cacheRetentionAnthropicModel(nil), cacheRetentionContext(), nil)
	last := req.Messages[len(req.Messages)-1]
	blocks, ok := last.Content.([]anthropicContentBlock)
	if !ok || len(blocks) == 0 || blocks[len(blocks)-1].CacheControl == nil || blocks[len(blocks)-1].CacheControl.Type != "ephemeral" {
		t.Fatalf("last message content=%#v", last.Content)
	}
	if len(req.Tools) != 1 || req.Tools[0].CacheControl == nil || req.Tools[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("tools=%#v", req.Tools)
	}
}

// Source: anthropic-message-threads.test.ts. The transport fixture keeps the
// original endpoint identity while making every request local to the test.
func TestSourceV160AnthropicThreadLifecycle(t *testing.T) {
	for _, mode := range []string{"alias", "gateway", "disabled-cache", "replay", "new-prefix", "cleanup", "expired", "unsupported", "failed-resend", "unrelated"} {
		t.Run(mode, func(t *testing.T) {
			var requests []map[string]any
			var betas []string
			model := &goai.Model{ID: "claude-opus-4-8", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: "https://api.anthropic.com", ContextWindow: 100000, MaxTokens: 100}
			if mode == "alias" {
				model.Provider = "anthropic-1-api"
			}
			if mode == "gateway" {
				model.BaseURL = "https://gateway.example.test"
			}
			session := t.Name() + "/fixed"
			defer goai.CleanupSessionResources(session)
			transport := anthropicThreadTestTransport(func(req *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					return nil, err
				}
				requests = append(requests, body)
				betas = append(betas, req.Header.Get("Anthropic-Beta"))
				if len(requests) == 2 || mode == "failed-resend" && len(requests) == 3 {
					status, code, message := 0, "", ""
					switch mode {
					case "expired":
						status, code, message = 404, "thread_not_found", "thread rejected"
					case "unsupported":
						status, code, message = 400, "thread_unsupported_request", "rejected"
					case "failed-resend":
						status, message = 400, "thread: Extra inputs are not permitted"
						if len(requests) == 3 {
							message = "Real problem"
						}
					case "unrelated":
						status, message = 400, "prompt is too long"
					}
					if status != 0 {
						payload := map[string]any{"type": "error", "error": map[string]any{"type": "invalid_request_error", "message": message}}
						if code != "" {
							payload["error"].(map[string]any)["details"] = map[string]any{"error_code": code}
						}
						wire, _ := json.Marshal(payload)
						return &http.Response{StatusCode: status, Request: req, Body: io.NopCloser(bytes.NewReader(wire)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
					}
				}
				data := fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_%d\",\"model\":\"claude-opus-4-8\",\"usage\":{\"input_tokens\":1}}}\n\n", len(requests)) +
					"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"ok\"}}\n\n" +
					"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
					"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
					"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
				return &http.Response{StatusCode: 200, Request: req, Body: io.NopCloser(strings.NewReader(data)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}, nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tools := []goai.Tool{{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}
			var payloads int
			turn := func(messages []goai.Message, warm bool, failures ...string) *goai.Message {
				var result *goai.Message
				opts := &goai.StreamOptions{APIKey: "test", SessionID: session, HTTPTransport: transport, DisableContinuation: warm,
					OnPayload: func(v any, _ *goai.Model) (any, error) { payloads++; return v, nil },
				}
				if mode == "disabled-cache" {
					opts.CacheRetention = goai.CacheRetentionNone
				}
				if warm {
					maxTokens := 1
					opts.MaxTokens = &maxTokens
				}
				for ev := range goai.Stream(ctx, model, &goai.Context{SystemPrompt: "prompt", Messages: messages, Tools: tools}, opts) {
					switch e := ev.(type) {
					case *goai.DoneEvent:
						result = e.Message
					case *goai.ErrorEvent:
						if len(failures) != 1 || e.Err == nil || !strings.Contains(e.Err.Error(), failures[0]) {
							t.Errorf("stream error: %v, want %v", e.Err, failures)
						}
						result = e.Error
						if result == nil {
							result = &goai.Message{StopReason: goai.StopReasonError}
						}
					}
				}
				want := goai.StopReasonStop
				if len(failures) != 0 {
					want = goai.StopReasonError
				}
				if result == nil || result.StopReason != want {
					t.Fatalf("turn = %+v", result)
				}
				return result
			}
			first := []goai.Message{goai.UserMessage("first")}
			reply := turn(first, false)
			if mode == "gateway" || mode == "disabled-cache" {
				if requests[0]["thread"] != nil || strings.Contains(betas[0], "message-threads-2026-08-12") {
					t.Fatalf("ineligible request = %v, %q", requests[0], betas[0])
				}
				return
			}
			if !reflect.DeepEqual(requests[0]["thread"], map[string]any{"type": "create"}) || requests[0]["system"] == nil || requests[0]["tools"] == nil || !strings.Contains(betas[0], "message-threads-2026-08-12") {
				t.Fatalf("create = %v, %q", requests[0], betas[0])
			}
			if mode == "replay" {
				turn(first, true)
				if requests[1]["thread"] != nil || requests[1]["max_tokens"] != float64(1) || !reflect.DeepEqual(requests[0]["messages"], requests[1]["messages"]) || betas[0] != betas[1] {
					t.Fatalf("warm request = %v", requests[1])
				}
			}
			if mode == "cleanup" {
				if err := goai.CleanupSessionResources(session); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "new-prefix" {
				first[0] = goai.UserMessage("edited")
			}
			second := append(first, *reply, goai.UserMessage("second"))
			var completed *goai.Message
			switch mode {
			case "failed-resend":
				completed = turn(second, false, "Real problem")
			case "unrelated":
				completed = turn(second, false, "prompt is too long")
			default:
				completed = turn(second, false)
			}
			if mode == "expired" || mode == "unsupported" || mode == "failed-resend" || mode == "unrelated" {
				wantRequests := 3
				if mode == "unrelated" {
					wantRequests = 2
				}
				if len(requests) != wantRequests || payloads != 2 || !reflect.DeepEqual(requests[1]["thread"], map[string]any{"type": "continue", "previous_message_id": "msg_1"}) {
					t.Fatalf("requests=%v payloads=%d", requests, payloads)
				}
				if mode == "unrelated" {
					if len(completed.Diagnostics) != 0 {
						t.Fatalf("unrelated diagnostic: %+v", completed.Diagnostics)
					}
					return
				}
				var wantThread any
				if mode == "expired" {
					wantThread = map[string]any{"type": "create"}
				}
				if !reflect.DeepEqual(requests[2]["thread"], wantThread) || len(requests[2]["messages"].([]any)) != 3 || requests[2]["system"] == nil || betas[2] != betas[1] {
					t.Fatalf("resend = %v", requests[2])
				}
				wantReason := map[string]string{"expired": "thread_not_found", "unsupported": "thread_unsupported_request", "failed-resend": "400"}[mode]
				if len(completed.Diagnostics) != 1 || completed.Diagnostics[0].Type != "anthropic_thread_fallback" || completed.Diagnostics[0].Details["reason"] != wantReason {
					t.Fatalf("fallback diagnostics = %+v", completed.Diagnostics)
				}
				if mode == "failed-resend" {
					turn(second, false)
				} else {
					turn(append(second, *completed, goai.UserMessage("third")), false)
				}
				var later any
				switch mode {
				case "expired":
					later = map[string]any{"type": "continue", "previous_message_id": "msg_3"}
				case "failed-resend":
					later = map[string]any{"type": "create"}
				}
				if !reflect.DeepEqual(requests[3]["thread"], later) || betas[3] != betas[0] {
					t.Fatalf("later request = %v", requests[3])
				}
				return
			}
			last := requests[len(requests)-1]
			if mode == "cleanup" || mode == "new-prefix" {
				if !reflect.DeepEqual(last["thread"], map[string]any{"type": "create"}) || len(last["messages"].([]any)) != 3 {
					t.Fatalf("renewed thread = %v", last)
				}
				return
			}
			want := []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "second", "cache_control": map[string]any{"type": "ephemeral"}}}}}
			if !reflect.DeepEqual(last["thread"], map[string]any{"type": "continue", "previous_message_id": "msg_1"}) || last["tools"] != nil || last["system"] != nil || !reflect.DeepEqual(last["messages"], want) || betas[len(betas)-1] != betas[0] {
				t.Fatalf("continue = %v, beta = %q", last, betas[len(betas)-1])
			}
		})
	}
}

type anthropicThreadTestTransport func(*http.Request) (*http.Response, error)

func (f anthropicThreadTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
