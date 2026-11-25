package anthropic

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

func anthropicThreadEffortReplyV160() string {
	start, tail, _ := strings.Cut(anthropicThreadReplyV160("msg_a", false), "\n\n")
	thinking := "event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"thought"}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig"}}` + "\n\n" +
		"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n"
	return start + "\n\n" + thinking + strings.ReplaceAll(tail, `"index":0`, `"index":1`)
}

// Source: pi v1.6.0 packages/ai/test/anthropic-message-threads.test.ts:279–302.
func TestSourceV160AnthropicThreadManagedEffortWire(t *testing.T) {
	const session = "thread-pinned-managed-effort"
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
		return nil, fmt.Errorf("Anthropic managed effort escaped its inert fixture")
	})
	var requests []map[string]any
	var statuses []int
	wire := anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		// As with the prefix fixtures, beta=true is SDK route metadata.
		if r.Method != http.MethodPost || strings.TrimSuffix(r.URL.String(), "?beta=true") != anthropicThreadWireBaseV160+"/v1/messages" {
			t.Errorf("thread target = %s %s, want original authority and path", r.Method, r.URL)
		}
		const wantBeta = "mid-conversation-output-config-2026-07-01,thinking-binding-controls-2026-08-01,message-threads-2026-08-12"
		if !slices.Equal(r.Header.Values("X-Api-Key"), []string{"test-key"}) || len(r.Header.Values("Authorization")) != 0 || !slices.Equal(r.Header.Values("Anthropic-Beta"), []string{wantBeta}) {
			t.Errorf("fixture key, authorization or beta headers changed: %v", r.Header)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		requests = append(requests, request)
		status, contentType, body := http.StatusOK, "text/event-stream", ""
		switch len(requests) {
		case 1:
			body = anthropicThreadEffortReplyV160()
		case 2:
			// L279's queued msg_a bypasses the fake server's fingerprint
			// registration; the following continue receives one rejection.
			status, contentType, body = http.StatusBadRequest, "application/json", `{"type":"error","error":{"type":"invalid_request_error","message":"fixed field changed","details":{"error_code":"thread_fingerprint_mismatch"}}}`
		case 3:
			body = anthropicThreadReplyV160("msg_3", false)
		default:
			return nil, fmt.Errorf("unexpected thread request %d", len(requests))
		}
		statuses = append(statuses, status)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	zero := 0
	opts := &goai.StreamOptions{APIKey: "test-key", SessionID: session, MaxRetries: &zero, HTTPTransport: wire,
		Env: goai.ProviderEnv{"PI_CACHE_RETENTION": "", "ANTHROPIC_AUTH_TOKEN": ""}}
	model := &goai.Model{ID: "claude-test", Name: "Test Claude", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
		BaseURL: anthropicThreadWireBaseV160, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 100, Reasoning: true,
		AnthropicCompat: &goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtr(true), SupportsMidConvoEffort: boolPtr(true)}}
	ctx := &goai.Context{SystemPrompt: "You are a test assistant.", Tools: []goai.Tool{{Name: "lookup", Description: "Tool lookup",
		Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}}
	turn := func(text, id string, effort goai.ThinkingLevel) *goai.Message {
		t.Helper()
		user := goai.UserMessage(text)
		user.Timestamp = 1
		ctx.Messages = append(ctx.Messages, user)
		opts.Reasoning = &effort
		got, err := goai.Complete(t.Context(), model, ctx, opts)
		if err != nil || got == nil || got.ResponseID != id || got.StopReason != goai.StopReasonStop || got.ProviderThinkingLevel != string(effort) || goai.ContentText(got.Content) != "answer" {
			t.Fatalf("turn = %#v, err=%v; want %s/stop/%s with answer", got, err, id, effort)
		}
		ctx.Messages = append(ctx.Messages, *got)
		return got
	}
	first := turn("first", "msg_a", goai.ThinkingHigh)
	if len(first.Content) != 2 || first.Content[0].Type != "thinking" || first.Content[0].Thinking != "thought" || first.Content[0].ThinkingSignature != "sig" {
		t.Fatalf("first thought = %#v", first.Content)
	}
	turn("second", "msg_3", goai.ThinkingLow)
	if len(requests) != 3 || !slices.Equal(statuses, []int{200, 400, 200}) {
		t.Fatalf("thread requests = %d, statuses = %v; want 3/[200 400 200]", len(requests), statuses)
	}

	const (
		firstCached  = `{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}}]}`
		secondCached = `{"role":"user","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}]}`
		high         = `{"role":"system","content":[],"output_config":{"effort":"high"}}`
		low          = `{"role":"system","content":[],"output_config":{"effort":"low"}}`
		create       = `{"type":"create"}`
		resume       = `{"type":"continue","previous_message_id":"msg_a"}`
		// As in R40, the old Go user message uses a text block, not a JS string.
		replay = `{"role":"user","content":[{"type":"text","text":"first"}]},` + high + `,{"role":"assistant","content":[{"type":"thinking","thinking":"thought","signature":"sig"},{"type":"text","text":"answer"}]}`
	)
	for i, body := range []string{
		anthropicThreadWireBodyV160(`[`+firstCached+`,`+high+`]`, create, true),
		anthropicThreadWireBodyV160(`[`+secondCached+`,`+low+`]`, resume, false),
		anthropicThreadWireBodyV160(`[`+replay+`,`+secondCached+`,`+low+`]`, create, true),
	} {
		body = strings.TrimSuffix(body, "}") + `,"thinking":{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"}},"output_config":{"effort":"low"}}`
		var want map[string]any
		if err := json.Unmarshal([]byte(body), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(requests[i], want) {
			got, _ := json.Marshal(requests[i])
			t.Errorf("request %d body = %s; want %s", i, got, body)
		}
	}
}
