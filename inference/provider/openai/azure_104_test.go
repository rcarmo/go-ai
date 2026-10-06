package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAzure104CompletionsEndpointDeploymentAndCatalogIdentity(t *testing.T) {
	var requestModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/chat/completions" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer azure-key" {
			t.Errorf("Azure completions wire: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		requestModel, _ = payload["model"].(string)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"response\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"response\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	model := &goai.Model{ID: "deepseek-v4-pro", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiOpenAICompletions, ContextWindow: 10000, MaxTokens: 100}
	hookModel := ""
	options := &goai.StreamOptions{APIKey: "azure-key", AzureBaseURL: server.URL + "/openai/v1", AzureDeploymentName: "my-deployment", OnPayload: func(payload any, m *goai.Model) (any, error) { hookModel = m.ID; return payload, nil }}
	var done *goai.DoneEvent
	for event := range streamOpenAI(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, options) {
		switch v := event.(type) {
		case *goai.DoneEvent:
			done = v
		case *goai.ErrorEvent:
			t.Fatal(v.Err)
		}
	}
	if done == nil || requestModel != "my-deployment" || model.ID != "deepseek-v4-pro" || hookModel != model.ID {
		t.Fatal("deployment mutated catalog identity", done, requestModel, hookModel)
	}
}

func TestAzure104DeepSeek9645ProductionWire(t *testing.T) {
	t.Setenv("PI_CACHE_RETENTION", "long")
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderAzureOpenAI, "deepseek-v4-pro")
	if model == nil {
		t.Fatal("missing pinned Azure model")
	}
	assistant := goai.Message{Role: goai.RoleAssistant, Provider: goai.ProviderAzureOpenAI, Api: goai.ApiOpenAICompletions, Model: model.ID, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "internal reasoning", ThinkingSignature: "reasoning_content"}, {Type: "text", Text: "answer"}}}
	conv := &goai.Context{SystemPrompt: "first", Messages: []goai.Message{goai.UserMessage("hello"), assistant, goai.Message{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "second"}}}, goai.UserMessage("again")}}
	for _, level := range []goai.ThinkingLevel{"", goai.ThinkingHigh, goai.ThinkingMax} {
		opts := &goai.StreamOptions{APIKey: "key", AzureBaseURL: server.URL, SessionID: "session", CacheRetention: goai.CacheRetentionLong}
		if level != "" {
			opts.Reasoning = &level
		}
		for event := range streamOpenAI(context.Background(), model, conv, opts) {
			if failure, ok := event.(*goai.ErrorEvent); ok {
				t.Fatal(failure.Err)
			}
		}
		want := "high"
		if level == "" {
			want = ""
		}
		if got, _ := payload["reasoning_effort"].(string); got != want {
			t.Fatalf("level=%s effort=%s", level, got)
		}
		for _, key := range []string{"thinking", "prompt_cache_key", "prompt_cache_retention"} {
			if _, ok := payload[key]; ok {
				t.Fatalf("Foundry rejects %s: %v", key, payload)
			}
		}
		messages := payload["messages"].([]any)
		if messages[0].(map[string]any)["role"] != "system" || messages[3].(map[string]any)["role"] != "system" {
			t.Fatal("system messages collapsed or developer role", messages)
		}
		if messages[2].(map[string]any)["reasoning_content"] != "internal reasoning" {
			t.Fatal("reasoning replay dropped", messages)
		}
	}
}

func TestAzure104DeepSeekGeneratedReasoningReplays(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderAzureOpenAI, "deepseek-v4-pro")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"generated reasoning\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	var done *goai.DoneEvent
	for event := range streamOpenAI(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "key", AzureBaseURL: server.URL}) {
		if failure, ok := event.(*goai.ErrorEvent); ok {
			t.Fatal(failure.Err)
		}
		if result, ok := event.(*goai.DoneEvent); ok {
			done = result
		}
	}
	if done == nil {
		t.Fatal("no completed assistant")
	}
	request := buildRequestBody(model, &goai.Context{Messages: []goai.Message{*done.Message}}, nil)
	if len(request.Messages) != 1 || request.Messages[0].ReasoningContent == nil || *request.Messages[0].ReasoningContent != "generated reasoning" {
		t.Fatal("generated reasoning did not replay", request.Messages)
	}
	for _, field := range []string{"reasoning", "reasoning_content", "reasoning_text"} {
		assistant := goai.Message{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "signed", ThinkingSignature: field}, {Type: "text", Text: "answer"}}}
		data, err := json.Marshal(buildRequestBody(model, &goai.Context{Messages: []goai.Message{assistant}}, nil))
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["messages"].([]any)[0].(map[string]any)[field] != "signed" {
			t.Fatal("reasoning field lost", field, string(data))
		}
	}
}
