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

type capturedAnthropicRequest struct {
	Headers http.Header
	Body    map[string]any
}

func captureAnthropicRequestForCompat(t *testing.T, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) capturedAnthropicRequest {
	t.Helper()
	var captured capturedAnthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured.Body); err != nil {
			t.Fatalf("decode body: %v\n%s", err, string(body))
		}
		captured.Headers = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	copyModel := *model
	copyModel.BaseURL = server.URL
	if opts == nil {
		opts = &goai.StreamOptions{}
	}
	if opts.APIKey == "" && len(opts.Headers) == 0 {
		opts.APIKey = "test-key"
	}
	for ev := range streamAnthropic(context.Background(), &copyModel, convCtx, opts) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	return captured
}

func lookupToolContext(tools ...goai.Tool) *goai.Context {
	return &goai.Context{Messages: []goai.Message{goai.UserMessage("Use the tool")}, Tools: tools}
}

func lookupTool() goai.Tool {
	return goai.Tool{Name: "lookup", Description: "Look up a value", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`)}
}

func firstAnthropicTool(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools in body: %#v", body["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first tool object: %#v", tools[0])
	}
	return tool
}

func boolPtrCompat(v bool) *bool { return &v }
