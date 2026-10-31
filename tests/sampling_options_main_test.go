package goai_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/anthropic"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

func captureSamplingMain(t *testing.T, model *goai.Model, opts goai.StreamOptions) map[string]any {
	t.Helper()
	var captured map[string]any
	sentinel := errors.New("sampling payload captured")
	opts.APIKey = "test-key"
	calls := 0
	opts.OnPayload = func(payload any, _ *goai.Model) (any, error) {
		calls++
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &captured); err != nil {
			t.Fatal(err)
		}
		return nil, sentinel
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	terminalErrors := 0
	for event := range goai.Stream(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &opts) {
		if e, ok := event.(*goai.ErrorEvent); ok && errors.Is(e.Err, sentinel) {
			terminalErrors++
		}
	}
	if captured == nil || calls != 1 || terminalErrors != 1 {
		t.Fatal("payload was not captured", calls, model.Api)
	}
	return captured
}
func samplingModelMain(api goai.Api) *goai.Model {
	return &goai.Model{ID: "custom-model", Api: api, Provider: "custom-provider", BaseURL: "http://127.0.0.1:9/v1", MaxTokens: 16384, ContextWindow: 128000, Input: []string{"text"}}
}
func assertSamplingValuesMain(t *testing.T, payload, want map[string]any) {
	t.Helper()
	for key, value := range want {
		actual, exists := payload[key]
		if value == nil {
			if exists {
				t.Fatal("sampling field not omitted", key, actual)
			}
			continue
		}
		if actual != value {
			t.Fatal("sampling mismatch", key, actual, value, payload)
		}
	}
}
func TestPinnedSamplingOptionsMain(t *testing.T) {
	t.Run("request-zero-values", func(t *testing.T) {
		p := captureSamplingMain(t, samplingModelMain(goai.ApiOpenAICompletions), goai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.95, "top_k": 0, "min_p": 0}})
		assertSamplingValuesMain(t, p, map[string]any{"top_p": 0.95, "top_k": float64(0), "min_p": float64(0)})
	})
	t.Run("omitted", func(t *testing.T) {
		p := captureSamplingMain(t, samplingModelMain(goai.ApiOpenAICompletions), goai.StreamOptions{})
		assertSamplingValuesMain(t, p, map[string]any{"temperature": nil, "top_p": nil})
	})
	for _, api := range []goai.Api{goai.ApiOpenAICompletions, goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		t.Run(string(api)+"/defaults", func(t *testing.T) {
			m := samplingModelMain(api)
			m.SamplingParams = map[string]any{"top_p": 0.95, "min_p": 0.05}
			p := captureSamplingMain(t, m, goai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.5}})
			assertSamplingValuesMain(t, p, map[string]any{"top_p": 0.5, "min_p": 0.05})
		})
		t.Run(string(api)+"/thinking", func(t *testing.T) {
			m := samplingModelMain(api)
			m.Reasoning = true
			m.SamplingParams = map[string]any{"temperature": 1, "top_p": 0.95}
			m.SamplingParamsByThinkingLevel = map[goai.ModelThinkingLevel]map[string]any{goai.ModelThinkingLevel(goai.ThinkingLow): {"temperature": 0.6, "top_k": 64}}
			low := goai.ThinkingLevel(goai.ThinkingLow)
			p := captureSamplingMain(t, m, goai.StreamOptions{Reasoning: &low, SamplingParams: map[string]any{"top_p": 0.5}})
			assertSamplingValuesMain(t, p, map[string]any{"temperature": 0.6, "top_p": 0.5, "top_k": float64(64)})
		})
	}
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		t.Run(string(api)+"/summary-only", func(t *testing.T) {
			m := samplingModelMain(api)
			m.Reasoning = true
			m.SamplingParamsByThinkingLevel = map[goai.ModelThinkingLevel]map[string]any{goai.ThinkingOff: {"temperature": 0.7}, goai.ModelThinkingLevel(goai.ThinkingMedium): {"temperature": 0.8}}
			p := captureSamplingMain(t, m, goai.StreamOptions{ReasoningSummary: "auto"})
			assertSamplingValuesMain(t, p, map[string]any{"temperature": 0.8})
			if reasoning, ok := p["reasoning"].(map[string]any); !ok || reasoning["effort"] != "medium" {
				t.Fatal("summary did not use medium", p)
			}
		})
	}
	t.Run("effective-thinking-clamp", func(t *testing.T) {
		m := samplingModelMain(goai.ApiOpenAICompletions)
		m.Reasoning = true
		m.ThinkingLevelMap = map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil}
		m.SamplingParams = map[string]any{"temperature": 1, "top_p": 0.95}
		m.SamplingParamsByThinkingLevel = map[goai.ModelThinkingLevel]map[string]any{goai.ModelThinkingLevel(goai.ThinkingHigh): {"temperature": 0.8, "top_k": 64}}
		low := goai.ThinkingLevel(goai.ThinkingLow)
		p := captureSamplingMain(t, m, goai.StreamOptions{Reasoning: &low})
		assertSamplingValuesMain(t, p, map[string]any{"temperature": 0.8, "top_p": 0.95, "top_k": float64(64)})
	})
	t.Run("off", func(t *testing.T) {
		m := samplingModelMain(goai.ApiOpenAICompletions)
		m.SamplingParamsByThinkingLevel = map[goai.ModelThinkingLevel]map[string]any{goai.ThinkingOff: {"temperature": 0.7}}
		p := captureSamplingMain(t, m, goai.StreamOptions{})
		assertSamplingValuesMain(t, p, map[string]any{"temperature": 0.7})
	})
	t.Run("named-field-override", func(t *testing.T) {
		temperature := 0.0
		p := captureSamplingMain(t, samplingModelMain(goai.ApiOpenAICompletions), goai.StreamOptions{Temperature: &temperature, SamplingParams: map[string]any{"temperature": 1}})
		assertSamplingValuesMain(t, p, map[string]any{"temperature": float64(1)})
	})
	t.Run("non-compatible-api", func(t *testing.T) {
		p := captureSamplingMain(t, samplingModelMain(goai.ApiAnthropicMessages), goai.StreamOptions{SamplingParams: map[string]any{"top_p": 0.9, "top_k": 40}})
		assertSamplingValuesMain(t, p, map[string]any{"top_p": nil, "top_k": nil})
	})
}

func TestResponsesSummarySamplingBoundariesMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, tc := range []struct {
			name      string
			reasoning bool
			level     *goai.ThinkingLevel
			want      float64
		}{{"nil", true, nil, 0.8}, {"empty", true, new(goai.ThinkingLevel), 0.8}, {"low", true, func() *goai.ThinkingLevel { v := goai.ThinkingLow; return &v }(), 0.6}, {"off", true, func() *goai.ThinkingLevel { v := goai.ThinkingLevel(goai.ThinkingOff); return &v }(), 0.7}, {"non-reasoning", false, nil, 0.7}} {
			t.Run(string(api)+"/"+tc.name, func(t *testing.T) {
				m := samplingModelMain(api)
				m.Reasoning = tc.reasoning
				m.SamplingParamsByThinkingLevel = map[goai.ModelThinkingLevel]map[string]any{"off": {"temperature": 0.7}, "low": {"temperature": 0.6}, "medium": {"temperature": 0.8}}
				opts := goai.StreamOptions{Reasoning: tc.level, ReasoningSummary: "auto"}
				p := captureSamplingMain(t, m, opts)
				assertSamplingValuesMain(t, p, map[string]any{"temperature": tc.want})
				opts.SamplingParams = map[string]any{"temperature": 0.1}
				p = captureSamplingMain(t, m, opts)
				assertSamplingValuesMain(t, p, map[string]any{"temperature": 0.1})
			})
		}
	}
}
