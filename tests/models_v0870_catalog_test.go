package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0870MetaAndRadiusCatalogs(t *testing.T) {
	goai.RegisterBuiltinModels()

	meta := requireModel(t, goai.ProviderMeta, "muse-spark-1.3")
	if meta.Api != goai.ApiOpenAIResponses || meta.BaseURL != "https://api.meta.ai/v1" || !meta.Reasoning {
		t.Fatalf("meta muse-spark-1.3 metadata=%#v", meta)
	}
	if meta.InputLimits == nil || meta.InputLimits.Images == nil || meta.InputLimits.Images.Resize == nil || meta.InputLimits.Images.Resize.JPEGQuality != 80 || meta.InputLimits.Images.Resize.MaxBytes != 4718592 {
		t.Fatalf("meta input limits=%#v", meta.InputLimits)
	}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderMeta, goai.ProviderEnv{"META_API_KEY": "meta-key"}); got != "meta-key" {
		t.Fatalf("meta env key=%q", got)
	}

	radius := requireModel(t, goai.Provider("radius"), "balanced")
	if radius.Api != goai.ApiPiMessages || radius.BaseURL != "https://radius.pi.dev/v1" || radius.Lab != "Moonshot AI" {
		t.Fatalf("radius balanced metadata=%#v", radius)
	}
	if radius.Enabled == nil || !*radius.Enabled || len(radius.Providers) == 0 || radius.Providers[0].Credential != "radius" {
		t.Fatalf("radius provider routing metadata=%#v enabled=%#v", radius.Providers, radius.Enabled)
	}
}

func TestV0870ImageInputLimitsAndPromptCacheMetadata(t *testing.T) {
	goai.RegisterBuiltinModels()

	anthropic := requireModel(t, goai.ProviderAnthropic, "claude-opus-5")
	if anthropic.PromptCache == nil || anthropic.PromptCache.Short != 300 || anthropic.PromptCache.Long != 3600 {
		t.Fatalf("anthropic prompt cache=%#v", anthropic.PromptCache)
	}
	if anthropic.InputLimits == nil || anthropic.InputLimits.MaxRequestBytes != 32*1024*1024 || anthropic.InputLimits.Images == nil || anthropic.InputLimits.Images.MaxPerRequest != 600 {
		t.Fatalf("anthropic image limits=%#v", anthropic.InputLimits)
	}

	openai := requireModel(t, goai.ProviderOpenAI, "gpt-6-astra")
	if openai.InputLimits == nil || openai.InputLimits.MaxRequestBytes != 512*1024*1024 || openai.InputLimits.Images == nil || openai.InputLimits.Images.MaxPerRequest != 1500 {
		t.Fatalf("openai image limits=%#v", openai.InputLimits)
	}

	bedrock := requireModel(t, goai.ProviderAmazonBedrock, "amazon.nova-2-lite-v1:0")
	if bedrock.InputLimits == nil || bedrock.InputLimits.Images == nil || bedrock.InputLimits.Images.MaxPerMessage != 20 || bedrock.InputLimits.Images.Resize == nil || bedrock.InputLimits.Images.Resize.MaxWidth != 2000 {
		t.Fatalf("bedrock image limits=%#v", bedrock.InputLimits)
	}
	bedrockStrict := requireModel(t, goai.ProviderAmazonBedrock, "anthropic.claude-sonnet-4-5-20250929-v1:0")
	if bedrockStrict.BedrockCompat == nil || bedrockStrict.BedrockCompat.SupportsStrictMode == nil || !*bedrockStrict.BedrockCompat.SupportsStrictMode {
		t.Fatalf("bedrock strict compat=%#v", bedrockStrict.BedrockCompat)
	}
}

func TestV0870MidConversationCompatFlags(t *testing.T) {
	goai.RegisterBuiltinModels()

	kimi := requireModel(t, goai.ProviderMoonshotAI, "kimi-k3")
	if kimi.CompletionsCompat == nil || kimi.CompletionsCompat.SupportsMidConvoSystemMessages == nil || !*kimi.CompletionsCompat.SupportsMidConvoSystemMessages || kimi.CompletionsCompat.SupportsMidConvoToolAdditions == nil || !*kimi.CompletionsCompat.SupportsMidConvoToolAdditions {
		t.Fatalf("kimi mid-convo compat=%#v", kimi.CompletionsCompat)
	}

	gpt := requireModel(t, goai.ProviderOpenAI, "gpt-6-astra")
	if gpt.ResponsesCompat == nil || gpt.ResponsesCompat.SupportsMidConvoSystemMessages == nil || !*gpt.ResponsesCompat.SupportsMidConvoSystemMessages {
		t.Fatalf("gpt-6-astra responses compat=%#v", gpt.ResponsesCompat)
	}

	claude := requireModel(t, goai.ProviderAnthropic, "claude-opus-5")
	if claude.AnthropicCompat == nil || claude.AnthropicCompat.SupportsMidConvoSystemMessages == nil || !*claude.AnthropicCompat.SupportsMidConvoSystemMessages || claude.AnthropicCompat.SupportsMidConvoToolChanges == nil || !*claude.AnthropicCompat.SupportsMidConvoToolChanges {
		t.Fatalf("claude mid-convo compat=%#v", claude.AnthropicCompat)
	}
}
