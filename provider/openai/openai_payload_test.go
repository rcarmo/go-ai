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

func TestStreamOpenAICloudflareAIGatewayHeadersAndURL(t *testing.T) {
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "gw")
	var gotAuth, gotCfAIG, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCfAIG = r.Header.Get("cf-aig-authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderCloudflareAIGateway, Api: goai.ApiOpenAICompletions, BaseURL: server.URL + "/{CLOUDFLARE_GATEWAY_ID}"}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	opts := &goai.StreamOptions{APIKey: "cf-key"}
	for range streamOpenAI(context.Background(), model, convCtx, opts) {
	}

	if gotPath != "/gw/chat/completions" {
		t.Fatalf("resolved Cloudflare URL path = %q", gotPath)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization should be omitted for Cloudflare AI Gateway, got %q", gotAuth)
	}
	if gotCfAIG != "Bearer cf-key" {
		t.Fatalf("cf-aig-authorization = %q", gotCfAIG)
	}
}

func TestBuildRequestBodyUsesCompatThinkingFormats(t *testing.T) {
	reasoning := goai.ThinkingHigh
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}

	deepseekMax := "max"
	deepseek := &goai.Model{
		ID:                "deepseek-v4-pro",
		Provider:          goai.ProviderDeepSeek,
		Api:               goai.ApiOpenAICompletions,
		BaseURL:           "https://api.deepseek.com",
		Reasoning:         true,
		ThinkingLevelMap:  map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingHigh): &deepseekMax},
		CompletionsCompat: &goai.OpenAICompletionsCompat{ThinkingFormat: "deepseek"},
	}
	deepReq := buildRequestBody(deepseek, convCtx, &goai.StreamOptions{Reasoning: &reasoning})
	if deepReq.Thinking["type"] != "enabled" || deepReq.ReasoningEffort != "max" {
		t.Fatalf("unexpected DeepSeek thinking payload: %#v effort=%q", deepReq.Thinking, deepReq.ReasoningEffort)
	}

	zaiToolStream := true
	zai := &goai.Model{
		ID:                "glm-4.7-flash",
		Provider:          goai.ProviderZAI,
		Api:               goai.ApiOpenAICompletions,
		BaseURL:           "https://api.z.ai/api/paas/v4",
		Reasoning:         true,
		CompletionsCompat: &goai.OpenAICompletionsCompat{ThinkingFormat: "zai", ZaiToolStream: &zaiToolStream},
	}
	zaiReq := buildRequestBody(zai, &goai.Context{Messages: convCtx.Messages, Tools: []goai.Tool{{Name: "tool", Description: "tool", Parameters: json.RawMessage(`{"type":"object"}`)}}}, &goai.StreamOptions{Reasoning: &reasoning})
	if zaiReq.EnableThinking == nil || !*zaiReq.EnableThinking || zaiReq.ToolStream == nil || !*zaiReq.ToolStream {
		t.Fatalf("unexpected ZAI payload enableThinking=%#v toolStream=%#v", zaiReq.EnableThinking, zaiReq.ToolStream)
	}
}

func TestProcessSSEStreamCapturesResponseModelAndCacheUsage(t *testing.T) {
	body := io.NopCloser(io.MultiReader(
		stringsReader("data: {\"id\":\"chatcmpl_1\",\"model\":\"actual-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"prompt_tokens_details\":{\"cached_tokens\":30,\"cache_write_tokens\":10},\"completion_tokens\":5,\"total_tokens\":105}}\n\n"),
		stringsReader("data: [DONE]\n\n"),
	))
	ch := make(chan goai.Event, 16)
	model := &goai.Model{ID: "requested-model", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}
	processSSEStream(body, model, ch)
	close(ch)

	var done *goai.DoneEvent
	for ev := range ch {
		if d, ok := ev.(*goai.DoneEvent); ok {
			done = d
		}
	}
	if done == nil {
		t.Fatal("missing done event")
	}
	msg := done.Message
	if msg.ResponseID != "chatcmpl_1" || msg.ResponseModel != "actual-model" {
		t.Fatalf("response metadata = id %q model %q", msg.ResponseID, msg.ResponseModel)
	}
	if msg.Usage.Input != 70 || msg.Usage.CacheRead != 20 || msg.Usage.CacheWrite != 10 || msg.Usage.Output != 5 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
}

func stringsReader(s string) io.Reader { return strings.NewReader(s) }
