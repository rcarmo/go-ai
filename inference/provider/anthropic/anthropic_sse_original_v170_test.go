package anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Pinned source: packages/ai/test/anthropic-sse-parsing.test.ts at
// fb0fe823539f6d04850f6e34748c01f01f212e1f.
func completeOriginalAnthropicSSE(t *testing.T, body string, model *goai.Model, mutate func(*goai.Context, *goai.StreamOptions)) (*goai.Message, error) {
	t.Helper()
	if model == nil {
		model = &goai.Model{ID: "claude-test", Name: "Claude Test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, MaxTokens: 128}
	}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	zero := 0
	opts := &goai.StreamOptions{APIKey: "test-key", MaxRetries: &zero, HTTPTransport: anthropicThreadTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	if mutate != nil {
		mutate(ctx, opts)
	}
	return goai.Complete(t.Context(), model, ctx, opts)
}

func originalAnthropicSSE(events ...[2]string) string {
	var body strings.Builder
	for _, event := range events {
		body.WriteString("event: " + event[0] + "\ndata: " + event[1] + "\n\n")
	}
	return body.String()
}

func TestAnthropicOriginalSSEFinalInputTransformations(t *testing.T) {
	body := originalAnthropicSSE(
		[2]string{"message_start", `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1,"output_tokens":0},"input_transformations":[{"type":"thinking_dropped","path":"messages.1.content.0","reason":"prefix_binding_mismatch"}]}}`},
		[2]string{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1},"input_transformations":[{"type":"thinking_dropped","path":"messages.3.content.0","reason":"model_binding_mismatch"}]}`},
		[2]string{"message_stop", `{"type":"message_stop"}`},
	)
	message, err := completeOriginalAnthropicSSE(t, body, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Diagnostics) != 1 || message.Diagnostics[0].Type != "anthropic_input_transformations" {
		t.Fatalf("diagnostics=%#v", message.Diagnostics)
	}
	want := []any{map[string]any{"type": "thinking_dropped", "path": "messages.3.content.0", "reason": "model_binding_mismatch"}}
	if got := message.Diagnostics[0].Details["transformations"]; !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		t.Fatalf("transformations=%s want %s", gotJSON, wantJSON)
	}
}

func TestAnthropicOriginalSSERepairsMalformedToolJSON(t *testing.T) {
	body := originalAnthropicSSE(
		[2]string{"message_start", `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1,"output_tokens":0}}}`},
		[2]string{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_test","name":"edit","input":{}}}`},
		[2]string{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"A\H\",\"text\":\"col1\tcol2\"}"}}`},
		[2]string{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		[2]string{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":1,"output_tokens":1}}`},
		[2]string{"message_stop", `{"type":"message_stop"}`},
	)
	message, err := completeOriginalAnthropicSSE(t, body, nil, func(ctx *goai.Context, _ *goai.StreamOptions) {
		ctx.Tools = []goai.Tool{{Name: "edit", Description: "Edit a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"text":{"type":"string"}}}`)}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(message.Content) != 1 || message.Content[0].Type != "toolCall" {
		t.Fatalf("content=%#v", message.Content)
	}
	want := map[string]any{"path": `A\H`, "text": "col1\tcol2"}
	if !reflect.DeepEqual(message.Content[0].Arguments, want) {
		t.Fatalf("arguments=%#v want %#v", message.Content[0].Arguments, want)
	}
}

func TestAnthropicOriginalSSEPreservesRefusalAndSensitiveErrors(t *testing.T) {
	for _, tc := range []struct {
		name, stop, details, want string
	}{
		{"refusal explanation", "refusal", `,"stop_details":{"type":"refusal","explanation":"blocked by policy"}`, "blocked by policy"},
		{"sensitive provider text", "sensitive", "", "Provider stopped with: sensitive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := originalAnthropicSSE(
				[2]string{"message_start", `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":1,"output_tokens":0}}}`},
				[2]string{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"` + tc.stop + `"` + tc.details + `},"usage":{"input_tokens":1,"output_tokens":1}}`},
				[2]string{"message_stop", `{"type":"message_stop"}`},
			)
			message, err := completeOriginalAnthropicSSE(t, body, nil, nil)
			if err == nil || err.Error() != tc.want || message == nil || message.StopReason != goai.StopReasonError || message.RawStopReason != tc.stop || message.ErrorMessage != tc.want {
				t.Fatalf("stop=%q raw=%q error=%q err=%v", message.StopReason, message.RawStopReason, message.ErrorMessage, err)
			}
		})
	}
}

func TestAnthropicOriginalSSERejectsFallbackAfterOutputBegins(t *testing.T) {
	body := originalAnthropicSSE(
		[2]string{"message_start", `{"type":"message_start","message":{"id":"m","model":"claude-test","usage":{"input_tokens":1,"output_tokens":0}}}`},
		[2]string{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"partial"}}`},
		[2]string{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		[2]string{"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"fallback","from":{"model":"claude-test"},"to":{"model":"claude-fallback"}}}`},
	)
	message, err := completeOriginalAnthropicSSE(t, body, nil, nil)
	if err == nil || message == nil || message.StopReason != goai.StopReasonError || !strings.Contains(message.ErrorMessage, "unsupported mid-output model fallback") {
		t.Fatalf("message=%#v err=%v", message, err)
	}
}

func TestAnthropicOriginalSSEIgnoresUnknownEventsAfterStop(t *testing.T) {
	body := originalAnthropicSSE(
		[2]string{"message_start", `{"type":"message_start","message":{"id":"m","usage":{"input_tokens":12,"output_tokens":0}}}`},
		[2]string{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"Hello"}}`},
		[2]string{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		[2]string{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":12,"output_tokens":5}}`},
		[2]string{"message_stop", `{"type":"message_stop"}`},
		[2]string{"done", `[DONE]`},
		[2]string{"proxy.stats", `not json`},
	)
	var observed int
	message, err := completeOriginalAnthropicSSE(t, body, nil, func(_ *goai.Context, opts *goai.StreamOptions) {
		opts.OnProviderStreamEvent = func(any, *goai.Model) error {
			observed++
			return nil
		}
	})
	if err != nil || message.StopReason != goai.StopReasonStop || len(message.Content) != 1 || message.Content[0].Text != "Hello" || observed != 5 {
		t.Fatalf("message=%#v observed=%d err=%v", message, observed, err)
	}
}
