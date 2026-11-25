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

// Source: pi v1.6.0 packages/ai/test/anthropic-message-threads.test.ts:342–348.
func TestSourceV160AnthropicThreadNoSuffixWire(t *testing.T) {
	const session = "thread-pinned-no-suffix"
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
		return nil, fmt.Errorf("Anthropic no-suffix turn escaped its inert fixture")
	})
	var requests []map[string]any
	var betas []string
	wire := anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		// The SDK adds beta=true; its query detail is not this wire comparison.
		if r.Method != http.MethodPost || strings.TrimSuffix(r.URL.String(), "?beta=true") != anthropicThreadWireBaseV160+"/v1/messages" {
			t.Errorf("thread target = %s %s, want original authority and path", r.Method, r.URL)
		}
		beta := r.Header.Get("Anthropic-Beta")
		if !slices.Equal(r.Header.Values("X-Api-Key"), []string{"test-key"}) || len(r.Header.Values("Authorization")) != 0 ||
			!slices.Contains(strings.Split(beta, ","), "message-threads-2026-08-12") {
			t.Errorf("fixture key, authorization or thread beta changed: %v", r.Header)
		}
		betas = append(betas, beta)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		requests = append(requests, body)
		if len(requests) > 3 {
			return nil, fmt.Errorf("unexpected extra no-suffix request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(anthropicThreadReplyV160(fmt.Sprintf("msg_%d", len(requests)), false)))}, nil
	})
	zero := 0
	opts := &goai.StreamOptions{APIKey: "test-key", SessionID: session, MaxRetries: &zero, HTTPTransport: wire,
		Env: goai.ProviderEnv{"PI_CACHE_RETENTION": "", "ANTHROPIC_AUTH_TOKEN": ""}}
	model := &goai.Model{ID: "claude-test", Name: "Test Claude", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
		BaseURL: anthropicThreadWireBaseV160, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 100}
	ctx := &goai.Context{SystemPrompt: "You are a test assistant.", Tools: []goai.Tool{{Name: "lookup", Description: "Tool lookup",
		Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}}
	turn := func(wantID string) *goai.Message {
		t.Helper()
		got, err := goai.Complete(t.Context(), model, ctx, opts)
		if err != nil || got == nil || got.ResponseID != wantID || got.StopReason != goai.StopReasonStop || goai.ContentText(got.Content) != "answer" {
			t.Fatalf("turn = %#v, err=%v; want %s/stop with answer", got, err, wantID)
		}
		return got
	}
	checkRequest := func(idx int, body string) {
		t.Helper()
		var want map[string]any
		if err := json.Unmarshal([]byte(body), &want); err != nil {
			t.Fatal(err)
		}
		if betas[idx] != betas[0] {
			t.Errorf("request %d changed beta list: %v", idx, betas)
		}
		if !reflect.DeepEqual(requests[idx], want) {
			got, _ := json.Marshal(requests[idx])
			t.Errorf("request %d body = %s; want %s", idx, got, body)
		}
	}
	first := goai.UserMessage("first")
	first.Timestamp = 1
	ctx.Messages = []goai.Message{first}
	initial := turn("msg_1")
	ctx.Messages = append(ctx.Messages, *initial)
	// Submit precisely the stored prefix: no new user message was appended.
	renewed := turn("msg_2")
	if len(requests) != 2 {
		t.Fatalf("no-suffix requests = %d, want two create requests", len(requests))
	}
	const create = `{"type":"create"}`
	checkRequest(0, anthropicThreadWireBodyV160(`[{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}}]}]`, create, true))
	// Historical user text is a block in Go and an uncached string in the SDK fixture.
	checkRequest(1, anthropicThreadWireBodyV160(`[{"role":"user","content":[{"type":"text","text":"first"}]},{"role":"assistant","content":[{"type":"text","text":"answer"}]}]`, create, true))

	// Supplemental: the recreate must become the next continuation head.
	second := goai.UserMessage("second")
	second.Timestamp = 1
	ctx.Messages = append(ctx.Messages, *renewed, second)
	turn("msg_3")
	if len(requests) != 3 {
		t.Fatalf("requests after continuing the renewed head = %d, want 3", len(requests))
	}
	checkRequest(2, anthropicThreadWireBodyV160(`[{"role":"user","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}]}]`, `{"type":"continue","previous_message_id":"msg_2"}`, false))
}
