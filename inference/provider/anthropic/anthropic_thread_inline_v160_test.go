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

// Source: pi v1.6.0 packages/ai/test/anthropic-message-threads.test.ts:304–319.
func TestSourceV160AnthropicThreadInlineToolWire(t *testing.T) {
	const session = "thread-pinned-inline-tool"
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
		return nil, fmt.Errorf("Anthropic inline thread escaped its inert fixture")
	})
	var requests []map[string]any
	var betas []string
	wire := anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		// The SDK adds beta=true, but the public authority/path stays the same.
		if r.Method != http.MethodPost || strings.TrimSuffix(r.URL.String(), "?beta=true") != anthropicThreadWireBaseV160+"/v1/messages" {
			t.Errorf("thread target = %s %s, want original authority and path", r.Method, r.URL)
		}
		beta := r.Header.Get("Anthropic-Beta")
		features := strings.Split(beta, ",")
		if !slices.Equal(r.Header.Values("X-Api-Key"), []string{"test-key"}) || len(r.Header.Values("Authorization")) != 0 ||
			!slices.Contains(features, "inline-tools-2026-09-15") || !slices.Contains(features, "message-threads-2026-08-12") {
			t.Errorf("fixture key, authorization or beta headers changed: %v", r.Header)
		}
		betas = append(betas, beta)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return nil, err
		}
		requests = append(requests, body)
		if len(requests) > 2 {
			return nil, fmt.Errorf("unexpected extra inline thread request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(anthropicThreadReplyV160(fmt.Sprintf("msg_%d", len(requests)), false)))}, nil
	})
	zero := 0
	opts := &goai.StreamOptions{APIKey: "test-key", SessionID: session, MaxRetries: &zero, HTTPTransport: wire,
		Env: goai.ProviderEnv{"PI_CACHE_RETENTION": "", "ANTHROPIC_AUTH_TOKEN": ""}}
	model := &goai.Model{ID: "claude-test", Name: "Test Claude", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
		BaseURL: anthropicThreadWireBaseV160, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 100,
		AnthropicCompat: &goai.AnthropicMessagesCompat{SupportsMidConvoSystemMessages: boolPtr(true), SupportsMidConvoToolChanges: boolPtr(true)}}
	tool := func(name string) goai.Tool {
		return goai.Tool{Name: name, Description: "Tool " + name, Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}
	}
	initial := goai.CreateInitialSystemMessage("You are a test assistant.", []goai.Tool{tool("lookup")})
	first := goai.UserMessage("first")
	first.Timestamp = 1
	ctx := &goai.Context{Messages: []goai.Message{*initial, first}}
	turn := func(wantID string) *goai.Message {
		t.Helper()
		got, err := goai.Complete(t.Context(), model, ctx, opts)
		if err != nil || got == nil || got.ResponseID != wantID || got.StopReason != goai.StopReasonStop || goai.ContentText(got.Content) != "answer" {
			t.Fatalf("turn = %#v, err=%v; want %s/stop with answer", got, err, wantID)
		}
		return got
	}
	reply := turn("msg_1")
	second := goai.UserMessage("second")
	second.Timestamp = 1
	ctx.Messages = append(ctx.Messages, *reply, goai.Message{Role: goai.RoleSystem,
		Content: []goai.ContentBlock{{Type: "text", Text: "Update"}}, ToolsAdded: []goai.Tool{tool("other")}, Timestamp: 2}, second)
	turn("msg_2")
	if len(requests) != 2 {
		t.Fatalf("inline thread requests = %d, want 2", len(requests))
	}
	if betas[0] != betas[1] {
		t.Errorf("beta list changed across continuation: %v", betas)
	}

	const firstCached = `[{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}}]}]`
	// Go keeps an uncached user text block; the pinned SDK fixture uses a string.
	const suffix = `[{"role":"user","content":[{"type":"text","text":"second"}]},{"role":"system","content":[{"type":"text","text":"Update"},{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"other","description":"Tool other","eager_input_streaming":true,"input_schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}}},"cache_control":{"type":"ephemeral"}}]}]`
	const placeholder = `{"name":"__pi_deferred_placeholder__","description":"Reserved placeholder. Never available. Never call this.","input_schema":{"type":"object","properties":{},"required":[]},"defer_loading":true}`
	for i, body := range []string{
		anthropicThreadWireBodyV160(firstCached, `{"type":"create"}`, true),
		anthropicThreadWireBodyV160(suffix, `{"type":"continue","previous_message_id":"msg_1"}`, false),
	} {
		var want map[string]any
		if err := json.Unmarshal([]byte(body), &want); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			var descriptor map[string]any
			if err := json.Unmarshal([]byte(placeholder), &descriptor); err != nil {
				t.Fatal(err)
			}
			want["tools"] = append(want["tools"].([]any), descriptor)
		}
		if !reflect.DeepEqual(requests[i], want) {
			gotBody, _ := json.Marshal(requests[i])
			wantBody, _ := json.Marshal(want)
			t.Errorf("request %d body = %s; want %s", i, gotBody, wantBody)
		}
	}
}

// Supplemental coverage for flushing before a reply, not just at the tail.
func TestAnthropicInlineUpdateFollowsPendingToolResultV160(t *testing.T) {
	model := &goai.Model{ID: "claude-test", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
		AnthropicCompat: &goai.AnthropicMessagesCompat{SupportsMidConvoSystemMessages: boolPtr(true), SupportsMidConvoToolChanges: boolPtr(true)}}
	initial := goai.CreateInitialSystemMessage("Prompt", []goai.Tool{inlineToolV101("lookup", "Lookup")})
	ctx := &goai.Context{Messages: []goai.Message{
		*initial, goai.UserMessage("find it"),
		{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonToolUse,
			Content: []goai.ContentBlock{{Type: "toolCall", ID: "toolu_1", Name: "lookup", Arguments: map[string]any{"query": "x"}}}},
		{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{inlineToolV101("other", "New tool")}},
		{Role: goai.RoleToolResult, ToolCallID: "toolu_1", ToolName: "lookup", Content: []goai.ContentBlock{{Type: "text", Text: "result"}}},
		{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonStop,
			Content: []goai.ContentBlock{{Type: "text", Text: "answer"}}},
	}}
	req := buildRequest(model, ctx, &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})
	var roles []string
	for _, message := range req.Messages {
		roles = append(roles, message.Role)
	}
	if want := []string{"user", "assistant", "user", "system", "assistant"}; !slices.Equal(roles, want) {
		t.Fatalf("inline update roles = %v, want %v", roles, want)
	}
	blocks, ok := req.Messages[2].Content.([]anthropicContentBlock)
	if !ok || len(blocks) != 1 || blocks[0].Type != "tool_result" || blocks[0].ToolUseID != "toolu_1" || blocks[0].Content != "result" || blocks[0].IsError == nil || *blocks[0].IsError {
		t.Errorf("pending tool result changed: %#v", req.Messages[2])
	}
	update, ok := req.Messages[3].Content.([]anthropicContentBlock)
	if !ok || len(update) != 1 || update[0].Type != "tool_addition" {
		t.Errorf("inline update changed: %#v", req.Messages[3])
	}
}
