package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestStreamOpenAIInvokesOnPayload(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: server.URL}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	var invoked bool
	opts := &goai.StreamOptions{
		APIKey: "test-key",
		OnPayload: func(payload interface{}, model *goai.Model) (interface{}, error) {
			invoked = true
			m := map[string]interface{}{}
			b, _ := json.Marshal(payload)
			_ = json.Unmarshal(b, &m)
			m["temperature"] = 0.42
			m["model"] = "overridden-model"
			return m, nil
		},
	}

	for range streamOpenAI(context.Background(), model, convCtx, opts) {
	}

	if !invoked {
		t.Fatal("OnPayload was not invoked")
	}
	if got["model"] != "overridden-model" {
		t.Fatalf("expected overridden model in wire payload, got %#v", got["model"])
	}
	if got["temperature"] != 0.42 {
		t.Fatalf("expected overridden temperature in wire payload, got %#v", got["temperature"])
	}
}
