package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 packages/ai/test/anthropic-message-threads.test.ts:350–382,
// the thread_already_continued variant (line 352).
func TestSourceV160AnthropicThreadAlreadyContinuedWire(t *testing.T) {
	testSourceV160AnthropicThreadRecoveryWire(t, "thread_already_continued", http.StatusBadRequest, "invalid_request_error")
}

// Source: the thread_not_found variant of the same declaration (line 351).
func TestSourceV160AnthropicThreadNotFoundWire(t *testing.T) {
	testSourceV160AnthropicThreadRecoveryWire(t, "thread_not_found", http.StatusNotFound, "not_found_error")
}

// Source: the thread_continue_in_flight variant (line 353). The fixture
// rejects a continuation before any SSE; it does not overlap two turns.
func TestSourceV160AnthropicThreadContinueInFlightWire(t *testing.T) {
	testSourceV160AnthropicThreadRecoveryWire(t, "thread_continue_in_flight", http.StatusBadRequest, "invalid_request_error")
}

// Source: the thread_fingerprint_mismatch variant (line 354). The fixture
// queues the rejection; it does not change any fixed request field.
func TestSourceV160AnthropicThreadFingerprintMismatchWire(t *testing.T) {
	testSourceV160AnthropicThreadRecoveryWire(t, "thread_fingerprint_mismatch", http.StatusBadRequest, "invalid_request_error")
}

func testSourceV160AnthropicThreadRecoveryWire(t *testing.T, errorCode string, rejectedStatus int, rejectedType string) {
	t.Helper()
	session := "thread-pinned-recovery-" + errorCode
	if err := goai.CleanupSessionResources(session); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := goai.CleanupSessionResources(session); err != nil {
			t.Error(err)
		}
	})
	ambient := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = ambient })
	http.DefaultTransport = anthropicThreadTestTransport(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("Anthropic thread recovery escaped its inert fixture")
	})
	var requests []map[string]any
	var betas []string
	var statuses []int
	wire := anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		// The SDK's beta=true query detail is outside this comparison.
		if r.Method != http.MethodPost || strings.TrimSuffix(r.URL.String(), "?beta=true") != anthropicThreadWireBaseV160+"/v1/messages" {
			t.Errorf("thread target = %s %s, want original authority and path", r.Method, r.URL)
		}
		beta := r.Header.Get("Anthropic-Beta")
		if !slices.Equal(r.Header.Values("X-Api-Key"), []string{"test-key"}) || len(r.Header.Values("Authorization")) != 0 ||
			!slices.Contains(strings.Split(beta, ","), "message-threads-2026-08-12") {
			t.Errorf("fixture key, authorization or thread beta changed: %v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		requests = append(requests, body)
		betas = append(betas, beta)
		if len(requests) > 4 {
			return nil, fmt.Errorf("unexpected extra thread recovery request")
		}
		status, contentType := http.StatusOK, "text/event-stream"
		response := anthropicThreadReplyV160(fmt.Sprintf("msg_%d", len(requests)), false)
		if len(requests) == 2 {
			status, contentType = rejectedStatus, "application/json"
			response = fmt.Sprintf(`{"type":"error","error":{"type":%q,"message":"thread rejected","details":{"error_code":%q}}}`, rejectedType, errorCode)
		}
		statuses = append(statuses, status)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	zero := 0
	opts := &goai.StreamOptions{APIKey: "test-key", SessionID: session, MaxRetries: &zero, HTTPTransport: wire,
		Env: goai.ProviderEnv{"PI_CACHE_RETENTION": "", "ANTHROPIC_AUTH_TOKEN": ""}}
	model := &goai.Model{ID: "claude-test", Name: "Test Claude", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
		BaseURL: anthropicThreadWireBaseV160, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 100}
	ctx := &goai.Context{SystemPrompt: "You are a test assistant.", Tools: []goai.Tool{{Name: "lookup", Description: "Tool lookup",
		Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}}
	turn := func(text, wantID string, options *goai.StreamOptions) *goai.Message {
		t.Helper()
		user := goai.UserMessage(text)
		user.Timestamp = 1
		ctx.Messages = append(ctx.Messages, user)
		got, err := goai.Complete(t.Context(), model, ctx, options)
		if err != nil || got == nil || got.ResponseID != wantID || got.StopReason != goai.StopReasonStop || goai.ContentText(got.Content) != "answer" {
			t.Fatalf("turn = %#v, err=%v; want %s/stop with answer", got, err, wantID)
		}
		ctx.Messages = append(ctx.Messages, *got)
		return got
	}
	turn("first", "msg_1", opts)
	var hooks []map[string]any
	recoveryOpts := *opts
	recoveryOpts.OnPayload = func(value any, _ *goai.Model) (any, error) {
		if len(requests) != 1 {
			t.Errorf("recovery hook after %d requests, want before the rejected attempt", len(requests))
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var body map[string]any
		if err := json.Unmarshal(encoded, &body); err != nil {
			return nil, err
		}
		hooks = append(hooks, body)
		return nil, nil
	}
	recovered := turn("second", "msg_3", &recoveryOpts)
	if len(hooks) != 1 {
		t.Fatalf("recovery hook calls = %d, want 1", len(hooks))
	}
	wantStatuses := []int{http.StatusOK, rejectedStatus, http.StatusOK}
	if len(requests) != 3 || !slices.Equal(statuses, wantStatuses) {
		t.Fatalf("recovery requests = %d statuses = %v, want %d/%v", len(requests), statuses, len(wantStatuses), wantStatuses)
	}
	if betas[2] != betas[1] {
		t.Errorf("recreated beta = %q, want %q", betas[2], betas[1])
	}
	if d := recovered.Diagnostics; len(d) != 1 || d[0].Type != "anthropic_thread_fallback" || d[0].Details["reason"] != errorCode {
		t.Errorf("fallback diagnostics = %#v, want one %s diagnostic", d, errorCode)
	}

	const create = `{"type":"create"}`
	const firstCached = `{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}}]}`
	const secondCached = `{"role":"user","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}]}`
	const thirdCached = `{"role":"user","content":[{"type":"text","text":"third","cache_control":{"type":"ephemeral"}}]}`
	// The historical uncached user remains a Go text block, not a JS string.
	const prefix = `{"role":"user","content":[{"type":"text","text":"first"}]},{"role":"assistant","content":[{"type":"text","text":"answer"}]}`
	readBody := func(encoded string) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal([]byte(encoded), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	checkBody := func(idx int, expected string) {
		t.Helper()
		if !reflect.DeepEqual(requests[idx], readBody(expected)) {
			got, _ := json.Marshal(requests[idx])
			t.Errorf("request %d body = %s; want %s", idx, got, expected)
		}
	}
	replay := anthropicThreadWireBodyV160(`[`+prefix+`,`+secondCached+`]`, create, true)
	wantHook := readBody(replay)
	delete(wantHook, "thread") // The hook precedes the provider's thread transformation.
	// The upstream hook sees SDK beta options; Go carries betas in the HTTP header.
	if !reflect.DeepEqual(hooks[0], wantHook) {
		t.Errorf("pretransport recovery hook = %#v, want %#v", hooks[0], wantHook)
	}
	checkBody(0, anthropicThreadWireBodyV160(`[`+firstCached+`]`, create, true))
	checkBody(1, anthropicThreadWireBodyV160(`[`+secondCached+`]`, `{"type":"continue","previous_message_id":"msg_1"}`, false))
	checkBody(2, replay)

	turn("third", "msg_4", opts)
	wantStatuses = append(wantStatuses, http.StatusOK)
	if len(requests) != 4 || !slices.Equal(statuses, wantStatuses) {
		t.Fatalf("recovered-head requests = %d statuses = %v, want %d/%v", len(requests), statuses, len(wantStatuses), wantStatuses)
	}
	checkBody(3, anthropicThreadWireBodyV160(`[`+thirdCached+`]`, `{"type":"continue","previous_message_id":"msg_3"}`, false))
}

// A separate cancellation probe: cancel only after the recreate enters the
// transport. The pinned rejection row neither cancels nor overlaps requests.
func TestAnthropicThreadCanceledRecreateLeavesNoHead(t *testing.T) {
	const session = "thread-in-flight-canceled-recreate"
	if err := goai.CleanupSessionResources(session); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := goai.CleanupSessionResources(session); err != nil {
			t.Error(err)
		}
	})
	caller, cancel := context.WithCancel(t.Context())
	defer cancel()
	var attempts []string
	wire := anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		var body struct {
			Thread struct {
				Type       string `json:"type"`
				PreviousID string `json:"previous_message_id"`
			} `json:"thread"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		attempts = append(attempts, body.Thread.Type+":"+body.Thread.PreviousID)
		n := len(attempts)
		switch n {
		case 1, 4, 5:
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(anthropicThreadReplyV160(fmt.Sprintf("msg_%d", n), false)))}, nil
		case 2:
			return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"thread rejected","details":{"error_code":"thread_continue_in_flight"}}}`))}, nil
		case 3:
			if err := r.Context().Err(); err != nil {
				t.Errorf("recreate context ended before caller cancellation: %v", err)
			}
			cancel()
			if err := r.Context().Err(); errors.Is(err, context.Canceled) {
				return nil, err
			}
			t.Errorf("recreate transport request did not inherit caller cancellation")
			return nil, context.Canceled
		default:
			return nil, fmt.Errorf("unexpected extra recreate request")
		}
	})
	zero := 0
	model := &goai.Model{ID: "claude-test", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
		BaseURL: anthropicThreadWireBaseV160, Input: []string{"text"}, MaxTokens: 100}
	opts := &goai.StreamOptions{APIKey: "test-key", SessionID: session, MaxRetries: &zero, HTTPTransport: wire,
		Env: goai.ProviderEnv{"PI_CACHE_RETENTION": "", "ANTHROPIC_AUTH_TOKEN": ""}}
	first := goai.UserMessage("first")
	head, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{first}}, opts)
	if err != nil || head == nil || head.ResponseID != "msg_1" || head.StopReason != goai.StopReasonStop {
		t.Fatalf("initial head = %#v, err=%v; want msg_1/stop", head, err)
	}
	canceled, err := goai.Complete(caller, model, &goai.Context{Messages: []goai.Message{first, *head, goai.UserMessage("second")}}, opts)
	if !errors.Is(err, context.Canceled) || canceled == nil || canceled.StopReason != goai.StopReasonAborted || canceled.ResponseID != "" {
		t.Fatalf("canceled recreate = %#v, err=%v; want aborted/context.Canceled with no head", canceled, err)
	}
	if d := canceled.Diagnostics; len(d) != 1 || d[0].Type != "anthropic_thread_fallback" || d[0].Details["reason"] != "thread_continue_in_flight" {
		t.Errorf("canceled recreate diagnostics = %#v; want original rejection diagnostic", d)
	}
	if !slices.Equal(attempts, []string{"create:", "continue:msg_1", "create:"}) {
		t.Fatalf("attempts on cancel = %v; want create, rejected continue, canceled create", attempts)
	}
	// The rejected continuation already consumed msg_1 locally. An aborted
	// recreate must not restore that head or publish an unfinished one.
	transcript := []goai.Message{first, *head, goai.UserMessage("third")}
	resumed, err := goai.Complete(t.Context(), model, &goai.Context{Messages: transcript}, opts)
	if err != nil || resumed == nil || resumed.ResponseID != "msg_4" || resumed.StopReason != goai.StopReasonStop ||
		!slices.Equal(attempts, []string{"create:", "continue:msg_1", "create:", "create:"}) {
		t.Fatalf("resumed = %#v err=%v attempts=%v; want fresh create without reusing msg_1", resumed, err, attempts)
	}
	transcript = append(transcript, *resumed, goai.UserMessage("fourth"))
	next, err := goai.Complete(t.Context(), model, &goai.Context{Messages: transcript}, opts)
	if err != nil || next == nil || next.ResponseID != "msg_5" || next.StopReason != goai.StopReasonStop ||
		!slices.Equal(attempts, []string{"create:", "continue:msg_1", "create:", "create:", "continue:msg_4"}) {
		t.Fatalf("next = %#v err=%v attempts=%v; want continuation from the fresh msg_4", next, err, attempts)
	}
}
