package openroutercost

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"math"
	"testing"
)

func TestReportedCostMainContract(t *testing.T) {
	tests := []struct {
		name, wire string
		want       float64
		applied    bool
	}{
		{"free", `{"cost":0,"is_byok":false}`, 0, true},
		{"charged includes server tools", `{"cost":0.4,"is_byok":false,"cost_details":{"server_tool_cost":0.3}}`, 0.4, true},
		{"byok total", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":0.9}}`, 1, true},
		{"byok components", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":null,"upstream_inference_prompt_cost":0.6,"upstream_inference_completions_cost":0.3}}`, 1, true},
		{"missing cost", `{"is_byok":false}`, 1, false},
		{"null cost", `{"cost":null,"is_byok":false}`, 1, false},
		{"negative", `{"cost":-1,"is_byok":false}`, 1, false},
		{"string cost", `{"cost":"0.1","is_byok":false}`, 1, false},
		{"overflow cost", `{"cost":1e999,"is_byok":false}`, 1, false},
		{"missing byok", `{"cost":0.1}`, 1, false},
		{"wrong byok type", `{"cost":0.1,"is_byok":"false"}`, 1, false},
		{"missing details", `{"cost":0.1,"is_byok":true}`, 1, false},
		{"null details", `{"cost":0.1,"is_byok":true,"cost_details":null}`, 1, false},
		{"wrong details type", `{"cost":0.1,"is_byok":true,"cost_details":[]}`, 1, false},
		{"missing total differs from null", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_prompt_cost":0.6,"upstream_inference_completions_cost":0.3}}`, 1, false},
		{"invalid total no fallback", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":-1,"upstream_inference_prompt_cost":0.6,"upstream_inference_completions_cost":0.3}}`, 1, false},
		{"missing component", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":null,"upstream_inference_prompt_cost":0.6}}`, 1, false},
		{"invalid component", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":null,"upstream_inference_prompt_cost":0.6,"upstream_inference_completions_cost":-1}}`, 1, false},
		{"overflow components", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":null,"upstream_inference_prompt_cost":1e308,"upstream_inference_completions_cost":1e308}}`, 1, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var reported Accounting
			if err := json.Unmarshal([]byte(test.wire), &reported); err != nil {
				t.Fatal(err)
			}
			estimated := false
			usage := &goai.Usage{Cost: goai.CostBreakdown{Input: .25, Output: .75, Total: 1, TotalFromProvider: &estimated}}
			Apply(usage, reported)
			if math.Abs(usage.Cost.Total-test.want) > 1e-12 || *usage.Cost.TotalFromProvider != test.applied || usage.Cost.Input != .25 || usage.Cost.Output != .75 {
				t.Fatalf("cost=%+v flag=%v", usage.Cost, *usage.Cost.TotalFromProvider)
			}
		})
	}
}

func TestModelAndCatalogueProvenanceMain(t *testing.T) {
	for _, test := range []struct {
		provider, base string
		want           bool
	}{{"openrouter", "https://example.com", true}, {"openrouter-work", "https://openrouter.ai/api/v1", true}, {"custom", "https://example.com/v1", false}} {
		if IsModel(&goai.Model{Provider: goai.Provider(test.provider), BaseURL: test.base}) != test.want {
			t.Fatal(test)
		}
	}
	billed := true
	usage := &goai.Usage{Input: 1, Output: 1, Cost: goai.CostBreakdown{Total: 10, TotalFromProvider: &billed}}
	cost := goai.CalculateCost(&goai.Model{Cost: goai.ModelCost{Input: 1, Output: 2}}, usage)
	if cost.TotalFromProvider == nil || *cost.TotalFromProvider || math.Abs(cost.Total-3e-6) > 1e-15 {
		t.Fatal(cost)
	}
}
