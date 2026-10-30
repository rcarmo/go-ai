package openai

import (
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"math"
	"strings"
	"testing"
)

func TestOpenRouterCostMainRawSSE(t *testing.T) {
	for _, test := range []struct {
		name, provider, base, reported string
		want                           float64
		billed                         bool
		choiceUsage                    bool
	}{
		{"native billed", "openrouter", "https://example.com", `"cost":0.123,"is_byok":false`, .123, true, false},
		{"custom endpoint free", "custom", "https://openrouter.ai/api/v1", `"cost":0,"is_byok":false`, 0, true, false},
		{"byok", "openrouter", "", `"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":0.9}`, 1, true, false},
		{"choice usage", "openrouter", "", `"cost":0.123,"is_byok":false`, .123, true, true},
		{"other provider ignores billing", "custom", "https://example.com", `"cost":0.123,"is_byok":false`, .002, false, false},
		{"wrong accounting types retain tokens", "openrouter", "", `"cost":"0.1","is_byok":"false"`, .002, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &goai.Model{ID: "test", Provider: goai.Provider(test.provider), BaseURL: test.base, Api: goai.ApiOpenAICompletions, Cost: goai.ModelCost{Input: 1, Output: 2}}
			usage := fmt.Sprintf(`{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500,%s}`, test.reported)
			var payload string
			if test.choiceUsage {
				payload = fmt.Sprintf(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop","usage":%s}]}`, usage)
			} else {
				payload = fmt.Sprintf(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":%s}`, usage)
			}
			var final *goai.Message
			processSSEStream(strings.NewReader("data: "+payload+"\n\ndata: [DONE]\n\n"), model, func(e goai.Event) {
				switch v := e.(type) {
				case *goai.DoneEvent:
					final = v.Message
				case *goai.ErrorEvent:
					t.Fatalf("stream: %v", v.Err)
				}
			})
			if final == nil || final.Usage == nil {
				t.Fatal("stream did not return usage")
			}
			if final.Usage.Input != 1000 || final.Usage.Output != 500 || math.Abs(final.Usage.Cost.Total-test.want) > 1e-12 || final.Usage.Cost.TotalFromProvider == nil || *final.Usage.Cost.TotalFromProvider != test.billed {
				t.Fatalf("message=%+v usage=%+v", final, final.Usage)
			}
			if final.Usage.Cost.Input != .001 || final.Usage.Cost.Output != .001 {
				t.Fatalf("catalogue components changed: %+v", final.Usage.Cost)
			}
		})
	}
}

func TestOpenRouterCostMainNextUsageRestoresEstimate(t *testing.T) {
	model := &goai.Model{Provider: "openrouter", Cost: goai.ModelCost{Input: 1}}
	usage := &goai.Usage{}
	raw := sseUsage{PromptTokens: 1000}
	raw.Cost = []byte("0.4")
	raw.IsBYOK = []byte("false")
	applyUsage(usage, &raw, model)
	if usage.Cost.Total != .4 || !*usage.Cost.TotalFromProvider {
		t.Fatal(usage.Cost)
	}
	applyUsage(usage, &sseUsage{PromptTokens: 1000}, model)
	if usage.Cost.Total != .001 || *usage.Cost.TotalFromProvider {
		t.Fatal(usage.Cost)
	}
}

func TestOpenRouterCostMainTieredComponents(t *testing.T) {
	model := &goai.Model{Provider: "openrouter", Cost: goai.ModelCost{Input: 1, Output: 2, Tiers: []goai.ModelCostTier{{InputTokensAbove: 500, Input: 2, Output: 4}}}}
	raw := sseUsage{PromptTokens: 1000, CompletionTokens: 500}
	usage := &goai.Usage{}
	applyUsage(usage, &raw, model)
	if math.Abs(usage.Cost.Total-.004) > 1e-15 || *usage.Cost.TotalFromProvider {
		t.Fatal(usage.Cost)
	}
	raw.Cost = []byte("0.123")
	raw.IsBYOK = []byte("false")
	applyUsage(usage, &raw, model)
	if math.Abs(usage.Cost.Total-.123) > 1e-15 || !*usage.Cost.TotalFromProvider || usage.Cost.Input != .002 || usage.Cost.Output != .002 {
		t.Fatal(usage.Cost)
	}
}

func TestOpenAIUsageMainNullishCachePrecedenceAndReasoning(t *testing.T) {
	for _, test := range []struct {
		name, raw                                    string
		input, read, write, output, reasoning, total int
	}{
		{"top-level Kimi cache", `{"prompt_tokens":100,"cached_tokens":30}`, 70, 30, 0, 0, 0, 100},
		{"zero hit beats top-level", `{"prompt_tokens":100,"cached_tokens":30,"prompt_cache_hit_tokens":0}`, 100, 0, 0, 0, 0, 100},
		{"zero details beats fallbacks", `{"prompt_tokens":100,"cached_tokens":30,"prompt_cache_hit_tokens":20,"prompt_tokens_details":{"cached_tokens":0}}`, 100, 0, 0, 0, 0, 100},
		{"null details falls back to hit", `{"prompt_tokens":100,"cached_tokens":30,"prompt_cache_hit_tokens":20,"prompt_tokens_details":{"cached_tokens":null}}`, 80, 20, 0, 0, 0, 100},
		{"null hit falls back to top-level", `{"prompt_tokens":100,"cached_tokens":30,"prompt_cache_hit_tokens":null}`, 70, 30, 0, 0, 0, 100},
		{"independent writes and derived total", `{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10},"completion_tokens":9,"completion_tokens_details":{"reasoning_tokens":7},"total_tokens":999}`, 60, 30, 10, 9, 7, 109},
		{"clamped input", `{"prompt_tokens":5,"prompt_tokens_details":{"cached_tokens":10,"cache_write_tokens":20},"completion_tokens":2}`, 0, 10, 20, 2, 0, 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":%s}`, test.raw)
			var message *goai.Message
			processSSEStream(strings.NewReader("data: "+payload+"\n\ndata: [DONE]\n\n"), &goai.Model{ID: "test", Provider: "openai", Api: goai.ApiOpenAICompletions}, func(e goai.Event) {
				if done, ok := e.(*goai.DoneEvent); ok {
					message = done.Message
				}
			})
			if message == nil || message.Usage == nil {
				t.Fatal("missing usage")
			}
			u := message.Usage
			if u.Input != test.input || u.CacheRead != test.read || u.CacheWrite != test.write || u.Output != test.output || u.Reasoning != test.reasoning || u.TotalTokens != test.total || !u.ReasoningSet || u.CacheWrite1hSet {
				t.Fatalf("usage=%+v", u)
			}
		})
	}
}
