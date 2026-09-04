package goai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/anthropic"
	_ "github.com/rcarmo/go-ai/inference/provider/google"
	_ "github.com/rcarmo/go-ai/inference/provider/mistral"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openaicodex"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

func TestV0850PreGenerationAuthErrorsEmitBeforeDispatch(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	apis := []goai.Api{
		goai.ApiAnthropicMessages,
		goai.ApiAzureOpenAIResponses,
		goai.ApiGoogleGenerativeAI,
		goai.ApiMistralConversations,
		goai.ApiOpenAICodexResponses,
		goai.ApiOpenAICompletions,
		goai.ApiOpenAIResponses,
	}
	for _, api := range apis {
		t.Run(string(api), func(t *testing.T) {
			hits.Store(0)
			model := &goai.Model{
				ID:            "test-model",
				Name:          "Test",
				Api:           api,
				Provider:      "test-provider",
				BaseURL:       server.URL,
				Reasoning:     false,
				Input:         []string{"text"},
				Cost:          goai.ModelCost{},
				ContextWindow: 1000,
				MaxTokens:     100,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			ch := goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{Env: goai.ProviderEnv{}})
			event, ok := <-ch
			if !ok {
				t.Fatal("stream closed without pre-generation error")
			}
			errEvent, ok := event.(*goai.ErrorEvent)
			if !ok {
				t.Fatalf("first event=%T, want ErrorEvent", event)
			}
			message := ""
			if errEvent.Err != nil {
				message = errEvent.Err.Error()
			}
			if errEvent.Reason != goai.StopReasonError || !strings.Contains(message, "No API key for provider: test-provider") {
				t.Fatalf("error event=%#v err=%q", errEvent, message)
			}
			for range ch {
			}
			if hits.Load() != 0 {
				t.Fatalf("provider dispatched %d HTTP requests before auth error", hits.Load())
			}
		})
	}
}
