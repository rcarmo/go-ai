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

const anthropicThreadWireBaseV160 = "https://api.anthropic.com"
const anthropicThreadWireSettingsV160 = `,"system":[{"type":"text","text":"You are a test assistant.","cache_control":{"type":"ephemeral"}}],"tools":[{"name":"lookup","description":"Tool lookup","eager_input_streaming":true,"input_schema":{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]},"cache_control":{"type":"ephemeral"}}]`

func anthropicThreadWireBodyV160(messages, thread string, full bool) string {
	body := `{"model":"claude-test","max_tokens":100,"stream":true,"messages":` + messages + `,"thread":` + thread
	if full {
		body += anthropicThreadWireSettingsV160
	}
	return body + "}"
}

func anthropicThreadReplyV160(id string, tool bool) string {
	frames := []string{
		fmt.Sprintf(`{"type":"message_start","message":{"id":%q,"model":"claude-test","usage":{"input_tokens":10,"output_tokens":0}}}`, id),
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer"}}`,
		`{"type":"content_block_stop","index":0}`,
	}
	stop := "end_turn"
	if tool {
		stop = "tool_use"
		frames = append(frames,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"value\":\"x\"}"}}`,
			`{"type":"content_block_stop","index":1}`,
		)
	}
	frames = append(frames, fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":1}}`, stop), `{"type":"message_stop"}`)
	var out strings.Builder
	for _, frame := range frames {
		event, _, _ := strings.Cut(strings.TrimPrefix(frame, `{"type":"`), `"`)
		fmt.Fprintf(&out, "event: %s\ndata: %s\n\n", event, frame)
	}
	return out.String()
}

// Source: pi v1.6.0 packages/ai/test/anthropic-message-threads.test.ts:232–277.
func TestSourceV160AnthropicThreadPrefixWire(t *testing.T) {
	for _, withTool := range []bool{false, true} {
		name := "cached-user-prefix"
		if withTool {
			name = "tool-result-prefix"
		}
		t.Run(name, func(t *testing.T) {
			session := "thread-pinned-prefix-" + name
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
				return nil, fmt.Errorf("Anthropic continuation escaped its inert fixture")
			})

			var requests []map[string]any
			var statuses []int
			var betas []string
			wire := anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				// The pinned SDK appends beta=true; the native HTTP adapter
				// enables the thread beta with a header instead.
				if r.Method != http.MethodPost || strings.TrimSuffix(r.URL.String(), "?beta=true") != anthropicThreadWireBaseV160+"/v1/messages" {
					t.Errorf("thread target = %s %s, want pinned messages authority and path", r.Method, r.URL)
				}
				beta := r.Header.Get("Anthropic-Beta")
				if !slices.Equal(r.Header.Values("X-Api-Key"), []string{"test-key"}) || len(r.Header.Values("Authorization")) != 0 ||
					!slices.Contains(strings.Split(beta, ","), "message-threads-2026-08-12") {
					t.Errorf("unexpected fixture auth or beta headers: %v", r.Header)
				}
				betas = append(betas, beta)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				requests = append(requests, body)
				request := len(requests)
				var response *http.Response
				if withTool && request == 2 {
					// The source's queued msg_tool reply bypasses fingerprint
					// registration. The following continue is rejected once.
					response = &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"fixed field changed","details":{"error_code":"thread_fingerprint_mismatch"}}}`))}
				} else {
					id := fmt.Sprintf("msg_%d", request)
					if withTool && request == 1 {
						id = "msg_tool"
					}
					response = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
						Body: io.NopCloser(strings.NewReader(anthropicThreadReplyV160(id, withTool && request == 1)))}
				}
				statuses = append(statuses, response.StatusCode)
				return response, nil
			})
			zero := 0
			opts := &goai.StreamOptions{APIKey: "test-key", SessionID: session, MaxRetries: &zero, HTTPTransport: wire,
				Env: goai.ProviderEnv{"PI_CACHE_RETENTION": "", "ANTHROPIC_AUTH_TOKEN": ""}}
			model := &goai.Model{ID: "claude-test", Name: "Test Claude", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic,
				BaseURL: anthropicThreadWireBaseV160, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 100}
			ctx := &goai.Context{SystemPrompt: "You are a test assistant.", Tools: []goai.Tool{{Name: "lookup", Description: "Tool lookup",
				Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`)}}}
			turn := func(wantID string, wantStop goai.StopReason) *goai.Message {
				t.Helper()
				got, err := goai.Complete(t.Context(), model, ctx, opts)
				if err != nil || got == nil || got.ResponseID != wantID || got.StopReason != wantStop || goai.ContentText(got.Content) != "answer" {
					t.Fatalf("turn = %#v, err=%v, want %s/%s with answer", got, err, wantID, wantStop)
				}
				ctx.Messages = append(ctx.Messages, *got)
				return got
			}
			appendUser := func(text string) {
				message := goai.UserMessage(text)
				message.Timestamp = 1
				ctx.Messages = append(ctx.Messages, message)
			}
			const create = `{"type":"create"}`
			var expected []string
			wantStatuses := []int{200, 200}
			if withTool {
				appendUser("look it up")
				first := turn("msg_tool", goai.StopReasonToolUse)
				if len(first.Content) != 2 || first.Content[1].Type != "toolCall" || first.Content[1].ID != "toolu_1" || first.Content[1].Name != "lookup" || !reflect.DeepEqual(first.Content[1].Arguments, map[string]any{"value": "x"}) {
					t.Fatalf("first tool reply = %#v", first.Content)
				}
				ctx.Messages = append(ctx.Messages, goai.Message{Role: goai.RoleToolResult, ToolCallID: "toolu_1", ToolName: "lookup",
					Content: []goai.ContentBlock{{Type: "text", Text: "result"}}, Timestamp: 1})
				turn("msg_3", goai.StopReasonStop)
				const result = `{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"result","is_error":false,"cache_control":{"type":"ephemeral"}}]}`
				expected = []string{
					anthropicThreadWireBodyV160(`[{"role":"user","content":[{"type":"text","text":"look it up","cache_control":{"type":"ephemeral"}}]}]`, create, true),
					anthropicThreadWireBodyV160(`[`+result+`]`, `{"type":"continue","previous_message_id":"msg_tool"}`, false),
					// Go user messages retain their block form when the marker
					// moves; the pinned JS fixture replays this text as a string.
					anthropicThreadWireBodyV160(`[{"role":"user","content":[{"type":"text","text":"look it up"}]},{"role":"assistant","content":[{"type":"text","text":"answer"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"value":"x"}}]},`+result+`]`, create, true),
				}
				wantStatuses = []int{200, 400, 200}
			} else {
				appendUser("first")
				turn("msg_1", goai.StopReasonStop)
				appendUser("second")
				turn("msg_2", goai.StopReasonStop)
				expected = []string{
					anthropicThreadWireBodyV160(`[{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}}]}]`, create, true),
					anthropicThreadWireBodyV160(`[{"role":"user","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}}]}]`, `{"type":"continue","previous_message_id":"msg_1"}`, false),
				}
			}
			if len(requests) != len(expected) || !slices.Equal(statuses, wantStatuses) {
				t.Fatalf("thread requests = %d, statuses = %v; want %d/%v", len(requests), statuses, len(expected), wantStatuses)
			}
			for i, expectedBody := range expected {
				if betas[i] != betas[0] {
					t.Errorf("request %d beta list = %q, want unchanged %q", i, betas[i], betas[0])
				}
				var want map[string]any
				if err := json.Unmarshal([]byte(expectedBody), &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(requests[i], want) {
					t.Errorf("request %d body = %#v, want %s", i, requests[i], expectedBody)
				}
			}
		})
	}
}

// Supplemental: plain results join text in order, while a richer result
// still takes the existing block-array path.
func TestAnthropicPlainToolResultProjectionV160(t *testing.T) {
	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, ContextWindow: 200000, MaxTokens: 100}
	for _, tc := range []struct {
		name, want string
		content    []goai.ContentBlock
		isError    bool
	}{
		{"ordered-text", `{"type":"tool_result","tool_use_id":"toolu_1","content":"left\nright","is_error":false}`,
			[]goai.ContentBlock{{Type: "text", Text: "left"}, {Type: "text", Text: "right"}}, false},
		{"annotated", `{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"left"},{"type":"text","text":"right"}],"is_error":true}`,
			[]goai.ContentBlock{{Type: "text", Text: "left", TextSignaturePresent: true}, {Type: "text", Text: "right"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &goai.Context{Messages: []goai.Message{
				goai.UserMessage("look it up"),
				{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID,
					Content: []goai.ContentBlock{{Type: "toolCall", ID: "toolu_1", Name: "lookup", Arguments: map[string]any{}}}},
				{Role: goai.RoleToolResult, ToolCallID: "toolu_1", ToolName: "lookup", Content: tc.content, IsError: tc.isError},
			}}
			request := buildRequest(model, ctx, &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})
			blocks := request.Messages[2].Content.([]anthropicContentBlock)
			if len(blocks) != 1 {
				t.Fatalf("tool result blocks = %#v", blocks)
			}
			got, err := json.Marshal(blocks[0])
			if err != nil || string(got) != tc.want {
				t.Fatalf("tool result = %s, err=%v; want %s", got, err, tc.want)
			}
		})
	}
}
