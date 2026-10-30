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
