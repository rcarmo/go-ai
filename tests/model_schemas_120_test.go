package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestModelSchemas120FixedOfficialFragments(t *testing.T) {
	cases := []struct {
		name, value string
		valid       bool
	}{
		{"ThinkingLevelSchema", `"high"`, true}, {"ThinkingLevelSchema", `"off"`, false}, {"ModelThinkingLevelSchema", `"off"`, true},
		{"ThinkingLevelMapSchema", `{"off":null,"high":"native"}`, true}, {"ThinkingLevelMapSchema", `{"high":42}`, false},
		{"ModelCostSchema", `{"input":1,"output":2,"cacheRead":0,"cacheWrite":0,"tiers":[{"inputTokensAbove":100,"input":2,"output":3,"cacheRead":0,"cacheWrite":0}]}`, true},
		{"ModelCostSchema", `{"input":1}`, false},
		{"ModelCostSchema", `{"input":1e-1000000000,"output":0,"cacheRead":0,"cacheWrite":0}`, false},
		{"ModelInputLimitsSchema", `{"images":{"resize":{"maxWidth":12,"jpegQuality":100},"maxPerRequest":3},"maxRequestBytes":12}`, true},
		{"ModelInputLimitsSchema", `{"images":{"resize":{"jpegQuality":101}}}`, false},
		{"ModelInputLimitsSchema", `{"maxRequestBytes":0}`, false}, {"ModelInputLimitsSchema", `{"maxRequestBytes":1.5}`, false},
		{"ModelInputLimitsSchema", `{"maxRequestBytes":9007199254740991.1}`, false},
		{"ModelPromptCacheSchema", `{"short":300,"long":3600}`, true}, {"ModelPromptCacheSchema", `{"long":0}`, false},
		{"ProviderCompatSchema", `{"supportsStore":false,"futureExtension":true}`, true},
		{"OpenAIResponsesCompatSchema", `{"sessionAffinityFormat":"openrouter"}`, true}, {"OpenAIResponsesCompatSchema", `{"sessionAffinityFormat":"wrong"}`, false},
	}
	for _, c := range cases {
		err := goai.ValidateModelSchema(c.name, []byte(c.value))
		if (err == nil) != c.valid {
			t.Fatalf("%s %s valid=%v err=%v", c.name, c.value, c.valid, err)
		}
	}
	first, e := goai.ModelSchema("ModelCostSchema")
	if e != nil {
		t.Fatal(e)
	}
	first[0] = 'x'
	again, e := goai.ModelSchema("ModelCostSchema")
	if e != nil || again[0] != '{' {
		t.Fatal("schema alias", e)
	}
	if _, e := goai.ModelSchema("Unknown"); e == nil {
		t.Fatal("unknown schema accepted")
	}
}
