package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicOAuthToolNameNormalizationTodowrite(t *testing.T) {
	runAnthropicOAuthToolNameNormalizationCase(t, "todowrite", "TodoWrite", "todowrite")
}

func TestAnthropicOAuthToolNameNormalizationBuiltinRead(t *testing.T) {
	runAnthropicOAuthToolNameNormalizationCase(t, "read", "Read", "read")
}

func TestAnthropicOAuthToolNameNormalizationDoesNotMapFindToGlob(t *testing.T) {
	runAnthropicOAuthToolNameNormalizationCase(t, "find", "find", "find")
}

func TestAnthropicOAuthToolNameNormalizationCustomToolPassesThrough(t *testing.T) {
	runAnthropicOAuthToolNameNormalizationCase(t, "my_custom_tool", "my_custom_tool", "my_custom_tool")
}

func runAnthropicOAuthToolNameNormalizationCase(t *testing.T, toolName, expectedOutboundName, expectedInboundName string) {
	t.Helper()

	var outboundToolName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v\n%s", err, string(body))
		}
		if len(payload.Tools) != 1 {
			t.Fatalf("expected one outbound tool, got %#v", payload.Tools)
		}
		outboundToolName = payload.Tools[0].Name

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"usage\":{\"input_tokens\":12,\"output_tokens\":0,\"cache_read_input_tokens\":0,\"cache_creation_input_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_test\",\"name\":" + mustJSONString(expectedOutboundName) + ",\"input\":{}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"input_tokens\":12,\"output_tokens\":5,\"cache_read_input_tokens\":0,\"cache_creation_input_tokens\":0}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	tool := goai.Tool{
		Name:        toolName,
		Description: "Test tool",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"input":{"type":"string","description":"Input value"}}}`),
	}
	model := &goai.Model{ID: "claude-sonnet-4-6", Provider: goai.ProviderGitHubCopilot, Api: goai.ApiAnthropicMessages, BaseURL: server.URL}
	convCtx := &goai.Context{
		SystemPrompt: "You are a helpful assistant. Use the requested tool.",
		Messages:     []goai.Message{goai.UserMessage("Use " + toolName + ".")},
		Tools:        []goai.Tool{tool},
	}

	var toolCallName string
	var done *goai.Message
	for ev := range streamAnthropic(context.Background(), model, convCtx, &goai.StreamOptions{APIKey: "oauth-token"}) {
		switch e := ev.(type) {
		case *goai.ToolCallEndEvent:
			if e.ContentIndex < len(e.Partial.Content) && e.Partial.Content[e.ContentIndex].Type == "toolCall" {
				toolCallName = e.Partial.Content[e.ContentIndex].Name
			}
		case *goai.DoneEvent:
			done = e.Message
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}

	if outboundToolName != expectedOutboundName {
		t.Fatalf("outbound tool name = %q, want %q", outboundToolName, expectedOutboundName)
	}
	if done == nil || done.StopReason != goai.StopReasonToolUse {
		t.Fatalf("response stopReason = %#v, want toolUse", done)
	}
	if toolCallName != expectedInboundName {
		t.Fatalf("tool call name = %q, want %q", toolCallName, expectedInboundName)
	}
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
