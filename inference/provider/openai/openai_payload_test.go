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

func TestStreamOpenAIMissingAPIKeyErrorMatchesUpstream(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://example.invalid/v1"}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	for ev := range streamOpenAI(context.Background(), model, convCtx, nil) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			if e.Err == nil || e.Err.Error() != "No API key for provider: openai" {
				t.Fatalf("unexpected missing-key error: %v", e.Err)
			}
			return
		}
	}
	t.Fatal("expected error event")
}

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

func TestStreamOpenAIUsesExplicitAuthHeaderWithoutAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: server.URL}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	for range streamOpenAI(context.Background(), model, convCtx, &goai.StreamOptions{Headers: map[string]string{"authorization": "Bearer custom"}}) {
	}

	if gotAuth != "Bearer custom" {
		t.Fatalf("Authorization = %q", gotAuth)
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

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1536–1578.
func TestV0842DeepSeekMixedCaseURLUsesMaxTokensField(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	custom := localCompletionsModelV160("custom-deepseek-model", "Custom DeepSeek Model")
	custom.Provider, custom.BaseURL = "custom-deepseek", "https://api.deepseek.com"
	upper := custom
	upper.ID, upper.Name, upper.BaseURL = "custom-uppercase-deepseek-model", "Custom Uppercase DeepSeek Model", "https://API.DeepSeek.COM"
	for _, tc := range []struct {
		model   *goai.Model
		catalog bool
	}{
		{promptCacheCatalogueV160(t, goai.ProviderDeepSeek, "deepseek-flash"), true},
		{promptCacheCatalogueV160(t, goai.ProviderDeepSeek, "deepseek-v4-pro"), true},
		{&custom, false}, {&upper, false},
	} {
		t.Run(tc.model.ID, func(t *testing.T) { assertCompletionsMaxTokensV160(t, tc.model, tc.catalog) })
	}
}

func TestBuildRequestBodyClampsPromptCacheKey(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1"}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	req := buildRequestBody(model, convCtx, &goai.StreamOptions{SessionID: strings.Repeat("あ", 80), CacheRetention: goai.CacheRetentionShort})
	if got, want := req.PromptCacheKey, strings.Repeat("あ", 64); got != want {
		t.Fatalf("prompt cache key = %q, want %q", got, want)
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
	if zaiReq.Thinking == nil || zaiReq.Thinking["type"] != "enabled" || zaiReq.ToolStream == nil || !*zaiReq.ToolStream {
		t.Fatalf("unexpected ZAI payload thinking=%#v toolStream=%#v", zaiReq.Thinking, zaiReq.ToolStream)
	}

	stringThinkingOff := "none"
	stringThinking := &goai.Model{
		ID:                "string-thinking-model",
		Provider:          goai.ProviderOpenAI,
		Api:               goai.ApiOpenAICompletions,
		BaseURL:           "https://example.com",
		Reasoning:         true,
		ThinkingLevelMap:  map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: &stringThinkingOff, goai.ModelThinkingLevel(goai.ThinkingHigh): &deepseekMax},
		CompletionsCompat: &goai.OpenAICompletionsCompat{ThinkingFormat: "string-thinking"},
	}
	stringReq := buildRequestBody(stringThinking, convCtx, &goai.StreamOptions{Reasoning: &reasoning})
	if stringReq.Thinking["type"] != "max" {
		t.Fatalf("unexpected string-thinking payload: %#v", stringReq.Thinking)
	}

	chatTemplate := &goai.Model{
		ID:               "chat-template-model",
		Provider:         goai.ProviderOpenAI,
		Api:              goai.ApiOpenAICompletions,
		BaseURL:          "https://example.com",
		Reasoning:        true,
		ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: &stringThinkingOff, goai.ModelThinkingLevel(goai.ThinkingHigh): &deepseekMax},
		CompletionsCompat: &goai.OpenAICompletionsCompat{ThinkingFormat: "chat-template", ChatTemplateKwargs: map[string]goai.ChatTemplateKwargValue{
			"enable_thinking": {Var: "thinking.enabled"},
			"effort":          {Var: "thinking.effort", OmitWhenOff: true},
			"literal":         {Value: "ok"},
		}},
	}
	chatReq := buildRequestBody(chatTemplate, convCtx, &goai.StreamOptions{Reasoning: &reasoning})
	if chatReq.ChatTemplateKwargs["enable_thinking"] != true || chatReq.ChatTemplateKwargs["effort"] != "max" || chatReq.ChatTemplateKwargs["literal"] != "ok" {
		t.Fatalf("unexpected chat-template kwargs: %#v", chatReq.ChatTemplateKwargs)
	}
	chatOffReq := buildRequestBody(chatTemplate, convCtx, nil)
	if chatOffReq.ChatTemplateKwargs["enable_thinking"] != false {
		t.Fatalf("expected disabled chat-template thinking, got %#v", chatOffReq.ChatTemplateKwargs)
	}
	if _, ok := chatOffReq.ChatTemplateKwargs["effort"]; ok {
		t.Fatalf("expected effort omitted when thinking off, got %#v", chatOffReq.ChatTemplateKwargs)
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
	processSSEStream(body, model, goai.NewAssistantEventSender(ch))
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
	// Pinned main treats cache reads and writes independently; cached_tokens
	// is not an inclusive total from which writes must be subtracted.
	if msg.Usage.Input != 60 || msg.Usage.CacheRead != 30 || msg.Usage.CacheWrite != 10 || msg.Usage.Output != 5 {
		t.Fatalf("usage = %+v", msg.Usage)
	}
}

func TestProcessSSEStreamAttachesPendingEncryptedReasoningDetails(t *testing.T) {
	body := io.NopCloser(io.MultiReader(
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.encrypted\",\"id\":\"call_1\",\"data\":\"secret\"}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"hi\\\"}\"}}]}}]}\n\n"),
		stringsReader("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"tool_calls\",\"delta\":{}}]}\n\n"),
		stringsReader("data: [DONE]\n\n"),
	))
	ch := make(chan goai.Event, 16)
	model := &goai.Model{ID: "requested-model", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}
	processSSEStream(body, model, goai.NewAssistantEventSender(ch))
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
	if len(done.Message.Content) != 2 || done.Message.Content[0].Type != "thinking" || done.Message.Content[1].Type != "toolCall" {
		t.Fatalf("unexpected content: %#v", done.Message.Content)
	}
	if got := done.Message.Content[0].ThinkingSignature; got != `[{"type":"reasoning.encrypted","id":"call_1","data":"secret"}]` {
		t.Fatalf("thinking signature = %q", got)
	}
	if got := done.Message.Content[1].ThoughtSignature; got != "" {
		t.Fatalf("tool call thought signature should not duplicate reasoning_details: %q", got)
	}
}

func stringsReader(s string) io.Reader { return strings.NewReader(s) }

// Source: openai-completions-tool-choice.test.ts. Capture the payload before
// I/O so registered host identities stay intact without a network request.
// Source: openai-completions-tool-choice.test.ts:1174-1206 (roles) and 1790-1795 (Copilot effort).
func TestSourceV160OpenAICompletionsInstructionRolesAndEffort(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	const azureURL = "https://my-resource.openai.azure.com/openai/v1"
	official := *promptCacheCatalogueV160(t, goai.ProviderOpenAI, "gpt-5.5")
	official.Api, official.ResponsesCompat, official.CompletionsCompat = goai.ApiOpenAICompletions, nil, nil
	customOfficial := official
	customOfficial.Provider = "custom" // Existing supplemental direct-host check.
	local := &goai.Model{Api: goai.ApiOpenAICompletions, ID: "qwen3.8-27b", Name: "Qwen3.8 27B",
		Provider: "local-vllm", BaseURL: "http://localhost:8000/v1", Reasoning: true, Input: []string{"text"}, ContextWindow: 128_000, MaxTokens: 8192}
	azureHost := *local
	azureHost.ID, azureHost.Name, azureHost.Provider, azureHost.BaseURL = "gpt-5.5", "GPT-5.5", "my-azure", azureURL
	azureDeepSeek := *promptCacheCatalogueV160(t, goai.ProviderAzureOpenAI, "deepseek-v4-pro")
	azureDeepSeek.BaseURL = azureURL
	for _, test := range []struct {
		model *goai.Model
		want  string
	}{
		{&official, "developer"},
		{&customOfficial, "developer"},
		{promptCacheCatalogueV160(t, goai.ProviderGroq, "qwen/qwen3.8-27b"), "system"},
		{local, "system"},
		{promptCacheCatalogueV160(t, goai.ProviderOpenRouter, "deepseek/deepseek-v4-pro"), "system"},
		{promptCacheCatalogueV160(t, goai.ProviderOpenRouter, "openai/gpt-5.2-codex"), "system"},
		{promptCacheCatalogueV160(t, goai.ProviderOpenRouter, "anthropic/claude-fable-5.1:batch"), "system"},
		{&azureDeepSeek, "system"},
		{&azureHost, "system"},
	} {
		t.Run(string(test.model.Provider)+"/"+test.model.ID, func(t *testing.T) {
			if test.model.Api != goai.ApiOpenAICompletions || !test.model.Reasoning {
				t.Fatalf("provider fixture: API=%q reasoning=%t, want Completions and reasoning", test.model.Api, test.model.Reasoning)
			}
			conv := &goai.Context{SystemPrompt: "Follow instructions.", Messages: []goai.Message{goai.UserMessage("Hi")}}
			level := goai.ThinkingLevel("")
			opts := goai.StreamOptions{Reasoning: &level, Env: goai.ProviderEnv{
				"AZURE_OPENAI_BASE_URL": "", "AZURE_OPENAI_RESOURCE_NAME": "", "AZURE_OPENAI_DEPLOYMENT_NAME_MAP": "",
			}}
			wire := captureCompletionsWireV160(t, test.model, conv, opts)
			messages, ok := wire.payload["messages"].([]any)
			if !ok || len(messages) == 0 {
				t.Fatalf("missing request messages: %#v", wire.payload["messages"])
			}
			first, _ := messages[0].(map[string]any)
			if first["role"] != test.want {
				t.Errorf("first instruction role: %#v, want %s", first, test.want)
			}
		})
	}
	for _, effort := range []goai.ThinkingLevel{goai.ThinkingLow, goai.ThinkingHigh} {
		t.Run("github-copilot/"+string(effort), func(t *testing.T) {
			model := promptCacheCatalogueV160(t, goai.ProviderGitHubCopilot, "gemini-4-argon")
			got := captureCompletionsEffortV160(t, model, completionsChoiceContextV160("Hi"), effort)
			got.field(t, "reasoning_effort", string(effort))
		})
	}
}
