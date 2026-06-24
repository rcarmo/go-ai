package openai

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

func TestOpenRouterCacheWriteReproUpstreamSimulated(t *testing.T) {
	var sawCacheControl bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if strings.Contains(string(body), `"cache_control":{"type":"ephemeral"}`) {
			sawCacheControl = true
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"prompt_tokens_details\":{\"cached_tokens\":0,\"cache_write_tokens\":42},\"completion_tokens\":1,\"total_tokens\":101}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{
		ID:       "google/gemini-2.5-flash",
		Provider: goai.ProviderOpenRouter,
		Api:      goai.ApiOpenAICompletions,
		BaseURL:  server.URL,
		Cost:     goai.ModelCost{Input: 1, Output: 1, CacheRead: 0.1, CacheWrite: 1.25},
	}
	maxTokens := 32
	temperature := 0.0
	opts := &goai.StreamOptions{
		APIKey:      "openrouter-key",
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		OnPayload: func(payload interface{}, model *goai.Model) (interface{}, error) {
			b, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			var params map[string]interface{}
			if err := json.Unmarshal(b, &params); err != nil {
				return nil, err
			}
			messages, _ := params["messages"].([]interface{})
			for i := len(messages) - 1; i >= 0; i-- {
				msg, _ := messages[i].(map[string]interface{})
				if msg["role"] != "user" {
					continue
				}
				switch content := msg["content"].(type) {
				case string:
					msg["content"] = []interface{}{map[string]interface{}{"type": "text", "text": content, "cache_control": map[string]interface{}{"type": "ephemeral"}}}
				case []interface{}:
					for j := len(content) - 1; j >= 0; j-- {
						part, _ := content[j].(map[string]interface{})
						if part["type"] == "text" {
							part["cache_control"] = map[string]interface{}{"type": "ephemeral"}
							break
						}
					}
				}
				break
			}
			return params, nil
		},
	}
	ctx := &goai.Context{
		SystemPrompt: strings.Repeat("Prompt-caching probe content. ", 80),
		Messages:     []goai.Message{goai.UserMessage("Reply with exactly: OK")},
	}

	var done *goai.DoneEvent
	for ev := range streamOpenAI(context.Background(), model, ctx, opts) {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if !sawCacheControl {
		t.Fatal("expected OnPayload to add OpenRouter cache_control marker")
	}
	if done == nil || done.Message == nil {
		t.Fatal("missing done message")
	}
	if got := done.Message.Usage.CacheWrite; got != 42 {
		t.Fatalf("cacheWrite = %d, want 42", got)
	}
	if got := done.Message.StopReason; got != goai.StopReasonStop {
		t.Fatalf("stopReason = %q, want stop", got)
	}
}
