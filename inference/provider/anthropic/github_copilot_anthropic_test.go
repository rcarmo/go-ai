package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGitHubCopilotAnthropicAppliesAdaptiveThinkingEffortOverrides(t *testing.T) {
	goai.RegisterBuiltinModels()
	opus47 := goai.GetModel(goai.ProviderGitHubCopilot, "claude-opus-4.7")
	if opus47 == nil {
		t.Fatal("missing claude-opus-4.7")
	}
	if got := opus47.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMinimal)]; got == nil || *got != "low" {
		t.Fatalf("opus47 minimal=%v, want low", got)
	}
	if got := opus47.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingXHigh)]; got == nil || *got != "xhigh" {
		t.Fatalf("opus47 xhigh=%v, want xhigh", got)
	}
	if !goai.SupportsXhigh(opus47) {
		t.Fatal("opus47 should support xhigh")
	}

	sonnet46 := goai.GetModel(goai.ProviderGitHubCopilot, "claude-sonnet-4.6")
	if sonnet46 == nil {
		t.Fatal("missing claude-sonnet-4.6")
	}
	if got := sonnet46.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMinimal)]; got == nil || *got != "low" {
		t.Fatalf("sonnet46 minimal=%v, want low", got)
	}
	if got := sonnet46.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingXHigh)]; got == nil || *got != "max" {
		t.Fatalf("sonnet46 xhigh=%v, want max", got)
	}
	if !goai.SupportsXhigh(sonnet46) {
		t.Fatal("sonnet46 should support xhigh")
	}
}

func TestGitHubCopilotAnthropicUsesBearerAuthCopilotHeadersAndValidPayload(t *testing.T) {
	request := captureGitHubCopilotAnthropicRequest(t, false)
	if got := request.Headers.Get("Authorization"); got != "Bearer tid_copilot_session_test_token" {
		t.Fatalf("Authorization=%q", got)
	}
	if got := request.Headers.Get("User-Agent"); !strings.Contains(got, "GitHubCopilotChat") {
		t.Fatalf("User-Agent=%q, want GitHubCopilotChat", got)
	}
	if got := request.Headers.Get("Copilot-Integration-Id"); got != "vscode-chat" {
		t.Fatalf("Copilot-Integration-Id=%q, want vscode-chat", got)
	}
	if got := request.Headers.Get("X-Initiator"); got != "user" {
		t.Fatalf("X-Initiator=%q, want user", got)
	}
	if got := request.Headers.Get("Openai-Intent"); got != "conversation-edits" {
		t.Fatalf("Openai-Intent=%q, want conversation-edits", got)
	}
	if beta := request.Headers.Get("Anthropic-Beta"); strings.Contains(beta, "fine-grained-tool-streaming") {
		t.Fatalf("Anthropic-Beta should not contain fine-grained-tool-streaming: %q", beta)
	}
	if request.Body["model"] != "claude-sonnet-4.6" {
		t.Fatalf("model=%#v, want claude-sonnet-4.6", request.Body["model"])
	}
	if request.Body["stream"] != true {
		t.Fatalf("stream=%#v, want true", request.Body["stream"])
	}
	if _, ok := request.Body["messages"].([]any); !ok {
		t.Fatalf("messages=%#v, want array", request.Body["messages"])
	}
}

func TestGitHubCopilotAnthropicOmitsInterleavedThinkingBetaForAdaptiveThinkingModels(t *testing.T) {
	request := captureGitHubCopilotAnthropicRequest(t, false)
	if beta := request.Headers.Get("Anthropic-Beta"); strings.Contains(beta, "interleaved-thinking-2025-05-14") {
		t.Fatalf("Anthropic-Beta should not contain interleaved-thinking beta: %q", beta)
	}
}

func captureGitHubCopilotAnthropicRequest(t *testing.T, _ bool) capturedAnthropicRequest {
	t.Helper()
	goai.RegisterBuiltinModels()
	base := goai.GetModel(goai.ProviderGitHubCopilot, "claude-sonnet-4.6")
	if base == nil {
		t.Fatal("missing claude-sonnet-4.6")
	}
	var captured capturedAnthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &captured.Body); err != nil {
			t.Fatalf("decode body: %v\n%s", err, string(body))
		}
		captured.Headers = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	model := *base
	model.BaseURL = server.URL
	ctx := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Hello")}}
	opts := &goai.StreamOptions{APIKey: "tid_copilot_session_test_token"}
	for ev := range streamAnthropic(context.Background(), &model, ctx, opts) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	return captured
}
