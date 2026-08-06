package openai

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBasetenReasoningPayloadAndEnv(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderBaseten, "zai-org/GLM-5.2")
	if model == nil {
		t.Fatal("missing Baseten GLM 5.2")
	}
	if model.Api != goai.ApiOpenAICompletions || model.BaseURL != "https://inference.baseten.co/v1" || !model.Reasoning {
		t.Fatalf("unexpected Baseten metadata: %#v", model)
	}
	if model.CompletionsCompat == nil || model.CompletionsCompat.ThinkingFormat != "baseten" || len(model.CompletionsCompat.ChatTemplateArgs) == 0 {
		t.Fatalf("missing Baseten compat metadata: %#v", model.CompletionsCompat)
	}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderBaseten, goai.ProviderEnv{"BASETEN_API_KEY": "test-key"}); got != "test-key" {
		t.Fatalf("Baseten env key=%q", got)
	}

	reasoning := goai.ThinkingHigh
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Reasoning: &reasoning})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	args, ok := payload["chat_template_args"].(map[string]any)
	if !ok || args["enable_thinking"] != true {
		t.Fatalf("expected Baseten chat_template_args enable_thinking=true, got %s", body)
	}
	if payload["reasoning_effort"] != "high" {
		t.Fatalf("expected Baseten reasoning_effort high, got %s", body)
	}
}

func TestOpenAIAdvancedSamplingParamsOverrideTypedFields(t *testing.T) {
	temp := 0.2
	max := 32
	req := buildRequestBody(&goai.Model{ID: "custom", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1", MaxTokens: 1000, SamplingParams: map[string]any{"top_p": 0.9, "temperature": 0.7}}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Temperature: &temp, MaxTokens: &max, SamplingParams: map[string]any{"top_k": float64(40), "temperature": 0.8}})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["temperature"] != 0.8 || payload["top_p"] != 0.9 || payload["top_k"] != float64(40) {
		t.Fatalf("sampling params did not merge/override last: %s", body)
	}
}

func TestOpenAIThinkingTokenBudgetLeavesAnswerRoom(t *testing.T) {
	reasoning := goai.ThinkingHigh
	max := 4096
	model := &goai.Model{ID: "glm", Provider: goai.ProviderZAI, Api: goai.ApiOpenAICompletions, BaseURL: "http://localhost:8000/v1", Reasoning: true, MaxTokens: 16384, CompletionsCompat: &goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: boolPtr(true), MaxTokensField: "max_tokens"}}
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Reasoning: &reasoning, MaxTokens: &max})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["thinking_token_budget"] != float64(4096-1024) {
		t.Fatalf("expected thinking_token_budget to leave answer room, got %s", body)
	}
}

func TestOpenAICompletionsAllowsMissingFinishReasonWhenCompatDisablesIt(t *testing.T) {
	body := strings.NewReader("data: {\"id\":\"chatcmpl\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	ch := make(chan goai.Event, 8)
	noFinish := false
	processSSEStream(body, &goai.Model{ID: "no-finish", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, CompletionsCompat: &goai.OpenAICompletionsCompat{SupportsFinishReason: &noFinish}}, ch)
	close(ch)
	var done *goai.DoneEvent
	for ev := range ch {
		if d, ok := ev.(*goai.DoneEvent); ok {
			done = d
		}
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if done == nil || done.Reason != goai.StopReasonStop || done.Message.Content[0].Text != "ok" {
		t.Fatalf("unexpected done event: %#v", done)
	}
}
