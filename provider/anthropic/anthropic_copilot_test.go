package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestNormalizeAnthropicBaseURLAddsV1(t *testing.T) {
	if got := normalizeAnthropicBaseURL("https://api.individual.githubcopilot.com"); got != "https://api.individual.githubcopilot.com/v1" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}
	if got := normalizeAnthropicBaseURL("https://api.anthropic.com/v1"); got != "https://api.anthropic.com/v1" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}
}

func TestStreamAnthropicUsesBearerForCopilot(t *testing.T) {
	var gotAuth, gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotUA = r.Header.Get("User-Agent")
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{}}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	model := &goai.Model{ID: "claude-sonnet-4", Provider: goai.ProviderGitHubCopilot, Api: goai.ApiAnthropicMessages, BaseURL: server.URL}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}
	ch := streamAnthropic(context.Background(), model, ctx, &goai.StreamOptions{APIKey: "tok"})
	for range ch {
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("expected bearer auth, got %q", gotAuth)
	}
	if gotUA == "" {
		t.Fatalf("expected copilot user-agent header")
	}
}

func TestBuildRequestJSONRoundTrip(t *testing.T) {
	model := &goai.Model{ID: "claude-sonnet-4", Provider: goai.ProviderGitHubCopilot, Api: goai.ApiAnthropicMessages}
	body := buildRequest(model, &goai.Context{SystemPrompt: "sys", Messages: []goai.Message{goai.UserMessage("hi")}}, nil)
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	if !bytes.Contains(b, []byte("\"messages\"")) {
		t.Fatalf("expected messages in body: %s", string(b))
	}
}
