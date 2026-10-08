package bedrock

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestBedrock110OpenAIThinkingAndHaiku55(t *testing.T) {
	for _, tc := range []struct {
		id            string
		level         goai.ThinkingLevel
		field, effort string
	}{
		{"openai.gpt-oss-120b-1:0", goai.ThinkingMinimal, "reasoning_effort", "low"},
		{"openai.gpt-oss-120b-1:0", goai.ThinkingXHigh, "reasoning_effort", "high"},
		{"openai.gpt-6.1-sol", goai.ThinkingMinimal, "reasoning", "low"},
		{"openai.gpt-6.1-sol", goai.ThinkingXHigh, "reasoning", "xhigh"},
	} {
		model := &goai.Model{ID: tc.id, Api: goai.ApiBedrockConverseStream, Provider: goai.ProviderAmazonBedrock, Reasoning: true, MaxTokens: 100}
		fields := bedrockAdditionalFields(t, model, tc.level)
		var got any = fields[tc.field]
		if tc.field == "reasoning" {
			got = got.(map[string]any)["effort"]
		}
		if got != tc.effort {
			t.Fatal(tc, fields)
		}
		assertNoKey(t, fields, "thinking")
		assertNoKey(t, fields, "anthropic_beta")
	}
	model := &goai.Model{ID: "global.anthropic.claude-haiku-5-5", Name: "Claude Haiku 5.5", Reasoning: true, MaxTokens: 100}
	fields := bedrockAdditionalFields(t, model, goai.ThinkingXHigh)
	assertMapString(t, fields["thinking"].(map[string]any), "type", "adaptive")
	assertMapString(t, fields["output_config"].(map[string]any), "effort", "xhigh")
	if !supportsPromptCaching(model, nil) {
		t.Fatal("Haiku5.5 prompt caching disabled")
	}
	effort := "minimal"
	model = &goai.Model{ID: "openai.gpt-6", Reasoning: true, MaxTokens: 100, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingHigh): &effort}}
	fields = bedrockAdditionalFields(t, model, goai.ThinkingHigh)
	assertMapString(t, fields["reasoning"].(map[string]any), "effort", "minimal")
	model.ID, model.Name = "openai.gpt-6", "GPT-OSS 120B"
	fields = bedrockAdditionalFields(t, model, goai.ThinkingMax)
	assertMapString(t, fields, "reasoning_effort", "high")
}
