package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0821ClaudeOpus5CatalogMetadata(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, id := range []string{"global.anthropic.claude-opus-5", "us.anthropic.claude-opus-5", "eu.anthropic.claude-opus-5", "au.anthropic.claude-opus-5"} {
		model := goai.GetModel(goai.ProviderAmazonBedrock, id)
		if model == nil || model.Api != goai.ApiBedrockConverseStream || !model.Reasoning || model.ContextWindow != 1_000_000 || model.MaxTokens != 128_000 {
			t.Fatalf("unexpected Bedrock Opus 5 metadata for %s: %#v", id, model)
		}
	}
	if model := goai.GetModel(goai.ProviderAmazonBedrock, "jp.anthropic.claude-opus-5"); model != nil {
		t.Fatalf("the Bedrock catalog should omit the retired Japan Opus 5 profile; got %#v", model)
	}
	if model := goai.GetModel(goai.ProviderAnthropic, "claude-opus-5"); model == nil || !model.Reasoning {
		t.Fatalf("unexpected Anthropic Opus 5 metadata: %#v", model)
	}
	if model := goai.GetModel(goai.ProviderKimiCoding, "k3-256k"); model == nil || model.ContextWindow != 262144 {
		t.Fatalf("unexpected Kimi k3-256k metadata: %#v", model)
	}
}
