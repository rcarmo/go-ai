package bedrock

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func bedrockAdditionalFields(t *testing.T, model *goai.Model, reasoning goai.ThinkingLevel, opts ...func(*goai.StreamOptions)) map[string]interface{} {
	t.Helper()
	o := &goai.StreamOptions{Reasoning: &reasoning}
	for _, apply := range opts {
		apply(o)
	}
	input := buildConverseInput(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, o)
	if input.AdditionalModelRequestFields == nil {
		t.Fatal("expected additional model request fields")
	}
	data, err := input.AdditionalModelRequestFields.MarshalSmithyDocument()
	if err != nil {
		t.Fatalf("marshal additional fields: %v", err)
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode additional fields: %v", err)
	}
	return fields
}

func assertMapString(t *testing.T, got map[string]interface{}, key string, want string) {
	t.Helper()
	if got[key] != want {
		t.Fatalf("%s = %#v, want %q in %#v", key, got[key], want, got)
	}
}

func assertNoKey(t *testing.T, got map[string]interface{}, key string) {
	t.Helper()
	if _, ok := got[key]; ok {
		t.Fatalf("unexpected %s in %#v", key, got)
	}
}

func TestUpstreamBedrockThinkingPayload(t *testing.T) {
	goai.RegisterBuiltinModels()
	high := goai.ThinkingHigh
	xhigh := goai.ThinkingXHigh

	opus48 := *goai.GetModel(goai.ProviderAmazonBedrock, "global.anthropic.claude-opus-4-6-v1")
	opus48.ID = "global.anthropic.claude-opus-4-8-v1"
	opus48.Name = "Claude Opus 4.8 (Global)"

	fields := bedrockAdditionalFields(t, &opus48, high)
	thinking := fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "adaptive")
	assertMapString(t, thinking, "display", "summarized")
	assertMapString(t, fields["output_config"].(map[string]interface{}), "effort", "high")
	assertNoKey(t, fields, "anthropic_beta")

	fields = bedrockAdditionalFields(t, &opus48, xhigh)
	thinking = fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "adaptive")
	assertMapString(t, thinking, "display", "summarized")
	assertMapString(t, fields["output_config"].(map[string]interface{}), "effort", "xhigh")
	assertNoKey(t, fields, "anthropic_beta")

	fable := goai.GetModel(goai.ProviderAmazonBedrock, "global.anthropic.claude-fable-5")
	fields = bedrockAdditionalFields(t, fable, high)
	thinking = fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "adaptive")
	assertMapString(t, thinking, "display", "summarized")
	assertMapString(t, fields["output_config"].(map[string]interface{}), "effort", "high")
	assertNoKey(t, fields, "anthropic_beta")

	fields = bedrockAdditionalFields(t, fable, xhigh)
	thinking = fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "adaptive")
	assertMapString(t, thinking, "display", "summarized")
	assertMapString(t, fields["output_config"].(map[string]interface{}), "effort", "xhigh")

	govSonnet := *goai.GetModel(goai.ProviderAmazonBedrock, "us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	govSonnet.ID = "us-gov.anthropic.claude-sonnet-4-5-20250929-v1:0"
	govSonnet.Name = "Claude Sonnet 4.5 (GovCloud)"
	fields = bedrockAdditionalFields(t, &govSonnet, high)
	thinking = fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "enabled")
	if thinking["budget_tokens"] != float64(16384) {
		t.Fatalf("budget_tokens = %#v, want 16384 in %#v", thinking["budget_tokens"], thinking)
	}
	assertNoKey(t, thinking, "display")
	beta := fields["anthropic_beta"].([]interface{})
	if len(beta) != 1 || beta[0] != "interleaved-thinking-2025-05-14" {
		t.Fatalf("anthropic_beta = %#v", beta)
	}

	fields = bedrockAdditionalFields(t, &opus48, high, func(o *goai.StreamOptions) { o.Region = "us-gov-west-1" })
	thinking = fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "adaptive")
	assertNoKey(t, thinking, "display")
	assertMapString(t, fields["output_config"].(map[string]interface{}), "effort", "high")
	assertNoKey(t, fields, "anthropic_beta")
}

func TestUpstreamBedrockApplicationInferenceProfileSupport(t *testing.T) {
	goai.RegisterBuiltinModels()
	high := goai.ThinkingHigh
	profileOpus := *goai.GetModel(goai.ProviderAmazonBedrock, "global.anthropic.claude-opus-4-6-v1")
	profileOpus.ID = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/my-profile"
	profileOpus.Name = "Claude Opus 4.6"

	fields := bedrockAdditionalFields(t, &profileOpus, high)
	thinking := fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "adaptive")
	assertMapString(t, thinking, "display", "summarized")
	assertMapString(t, fields["output_config"].(map[string]interface{}), "effort", "high")

	profileSonnet46 := profileOpus
	profileSonnet46.Name = "Claude Sonnet 4.6"
	input := buildConverseInput(&profileSonnet46, &goai.Context{SystemPrompt: "You are helpful.", Messages: []goai.Message{goai.UserMessage("Hello")}}, &goai.StreamOptions{})
	if len(input.System) != 2 {
		t.Fatalf("system blocks = %d, want 2", len(input.System))
	}
	if len(input.Messages) != 1 || len(input.Messages[0].Content) < 2 {
		t.Fatalf("expected user message cache point, got %#v", input.Messages)
	}

	profileSonnet45 := *goai.GetModel(goai.ProviderAmazonBedrock, "us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	profileSonnet45.ID = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/my-profile"
	profileSonnet45.Name = "Claude Sonnet 4.5"
	fields = bedrockAdditionalFields(t, &profileSonnet45, high)
	thinking = fields["thinking"].(map[string]interface{})
	assertMapString(t, thinking, "type", "enabled")
	if _, ok := thinking["budget_tokens"]; !ok {
		t.Fatalf("expected budget_tokens in %#v", thinking)
	}
	beta := fields["anthropic_beta"].([]interface{})
	if len(beta) != 1 || beta[0] != "interleaved-thinking-2025-05-14" {
		t.Fatalf("anthropic_beta = %#v", beta)
	}
}
