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

func captureOpenAISamplingPayload(t *testing.T, model *goai.Model, opts *goai.StreamOptions) map[string]any {
	t.Helper()
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestOpenAIAdvancedSamplingParamsUpstreamMatrix(t *testing.T) {
	temp := 0.2
	max := 32
	baseModel := &goai.Model{ID: "custom", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1", MaxTokens: 1000}

	t.Run("preserves zero values", func(t *testing.T) {
		payload := captureOpenAISamplingPayload(t, baseModel, &goai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.95, "top_k": 0, "min_p": 0}})
		if payload["top_p"] != 0.95 || payload["top_k"] != float64(0) || payload["min_p"] != float64(0) {
			t.Fatalf("zero sampling values not preserved: %#v", payload)
		}
	})
	t.Run("omits absent params", func(t *testing.T) {
		payload := captureOpenAISamplingPayload(t, baseModel, nil)
		if _, ok := payload["top_p"]; ok {
			t.Fatalf("top_p should be omitted: %#v", payload)
		}
	})
	t.Run("applies model defaults", func(t *testing.T) {
		model := *baseModel
		model.SamplingParams = map[string]any{"temperature": 1.0, "top_p": 0.95}
		payload := captureOpenAISamplingPayload(t, &model, nil)
		if payload["temperature"] != 1.0 || payload["top_p"] != 0.95 {
			t.Fatalf("model defaults missing: %#v", payload)
		}
	})
	t.Run("request overrides model keys", func(t *testing.T) {
		model := *baseModel
		model.SamplingParams = map[string]any{"top_p": 0.95, "min_p": 0.05}
		payload := captureOpenAISamplingPayload(t, &model, &goai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.5}})
		if payload["top_p"] != 0.5 || payload["min_p"] != 0.05 {
			t.Fatalf("request override failed: %#v", payload)
		}
	})
	t.Run("overrides typed fields last", func(t *testing.T) {
		model := *baseModel
		model.SamplingParams = map[string]any{"top_p": 0.9, "temperature": 0.7}
		payload := captureOpenAISamplingPayload(t, &model, &goai.StreamOptions{Temperature: &temp, MaxTokens: &max, SamplingParams: map[string]any{"top_k": float64(40), "temperature": 0.8}})
		if payload["temperature"] != 0.8 || payload["top_p"] != 0.9 || payload["top_k"] != float64(40) {
			t.Fatalf("sampling params did not merge/override last: %#v", payload)
		}
	})
}

func captureThinkingTokenBudgetPayload(t *testing.T, model *goai.Model, reasoning *goai.ThinkingLevel, budgets *goai.ThinkingBudgets, maxTokens *int) map[string]any {
	t.Helper()
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Reasoning: reasoning, ThinkingBudgets: budgets, MaxTokens: maxTokens})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestOpenAIThinkingTokenBudgetUpstreamMatrix(t *testing.T) {
	yes := true
	baseModel := &goai.Model{ID: "glm", Provider: goai.ProviderZAI, Api: goai.ApiOpenAICompletions, BaseURL: "http://localhost:8000/v1", Reasoning: true, MaxTokens: 16384, CompletionsCompat: &goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: &yes, MaxTokensField: "max_tokens"}}
	level := func(v goai.ThinkingLevel) *goai.ThinkingLevel { return &v }
	max := func(v int) *int { return &v }
	budgets := func(minimal, low, medium, high *int) *goai.ThinkingBudgets {
		return &goai.ThinkingBudgets{Minimal: minimal, Low: low, Medium: medium, High: high}
	}
	intptr := func(v int) *int { return &v }

	tests := []struct {
		name      string
		model     *goai.Model
		reasoning *goai.ThinkingLevel
		budgets   *goai.ThinkingBudgets
		maxTokens *int
		want      any
	}{
		{"configured medium budget", baseModel, level(goai.ThinkingMedium), budgets(nil, nil, intptr(4096), nil), nil, float64(4096)},
		{"capability disabled omits", &goai.Model{ID: "glm", Provider: goai.ProviderZAI, Api: goai.ApiOpenAICompletions, BaseURL: "http://localhost:8000/v1", Reasoning: true, MaxTokens: 16384, CompletionsCompat: &goai.OpenAICompletionsCompat{ThinkingFormat: "zai"}}, level(goai.ThinkingMedium), budgets(nil, nil, intptr(4096), nil), nil, nil},
		{"reasoning off omits", baseModel, nil, budgets(nil, nil, nil, intptr(8192)), nil, nil},
		{"xhigh clamps to high", baseModel, level(goai.ThinkingXHigh), budgets(nil, nil, nil, intptr(8192)), nil, float64(8192)},
		{"max clamps to high", baseModel, level(goai.ThinkingMax), budgets(nil, nil, nil, intptr(8192)), nil, float64(8192)},
		{"default minimal", baseModel, level(goai.ThinkingMinimal), nil, nil, float64(1024)},
		{"default low", baseModel, level(goai.ThinkingLow), nil, nil, float64(2048)},
		{"default medium", baseModel, level(goai.ThinkingMedium), nil, nil, float64(8192)},
		{"default high leaves answer room at model ceiling", baseModel, level(goai.ThinkingHigh), nil, nil, float64(16384 - 1024)},
		{"caller max lower than model ceiling", baseModel, level(goai.ThinkingHigh), budgets(nil, nil, nil, intptr(8192)), max(4096), float64(4096 - 1024)},
		{"tiny ceiling omits zero budget", baseModel, level(goai.ThinkingHigh), budgets(nil, nil, nil, intptr(8192)), max(512), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := captureThinkingTokenBudgetPayload(t, tc.model, tc.reasoning, tc.budgets, tc.maxTokens)
			if got := payload["thinking_token_budget"]; got != tc.want {
				t.Fatalf("thinking_token_budget=%#v, want %#v; payload=%#v", got, tc.want, payload)
			}
		})
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
