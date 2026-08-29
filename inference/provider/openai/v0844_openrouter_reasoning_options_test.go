package openai

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func strPtrForOpenRouterTest(v string) *string { return &v }

func TestV0844OpenRouterReasoningPayloadSemantics(t *testing.T) {
	mandatory := &goai.Model{ID: "stealth/ox-alpha", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions, BaseURL: "https://openrouter.ai/api/v1", Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{
		goai.ThinkingOff: nil,
		goai.ModelThinkingLevel(goai.ThinkingMinimal): nil,
		goai.ModelThinkingLevel(goai.ThinkingLow):     strPtrForOpenRouterTest("low"),
		goai.ModelThinkingLevel(goai.ThinkingMedium):  nil,
		goai.ModelThinkingLevel(goai.ThinkingHigh):    strPtrForOpenRouterTest("high"),
		goai.ModelThinkingLevel(goai.ThinkingXHigh):   nil,
		goai.ModelThinkingLevel(goai.ThinkingMax):     strPtrForOpenRouterTest("max"),
	}}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}
	if req := buildRequestBody(mandatory, ctx, &goai.StreamOptions{}); req.Reasoning != nil {
		t.Fatalf("mandatory background call should omit reasoning, got %#v", req.Reasoning)
	}
	low := goai.ThinkingLow
	if req := buildRequestBody(mandatory, ctx, &goai.StreamOptions{Reasoning: &low}); req.Reasoning == nil || req.Reasoning["effort"] != "low" {
		t.Fatalf("explicit low reasoning=%#v, want effort low", req.Reasoning)
	}

	optional := &goai.Model{ID: "optional", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions, BaseURL: "https://openrouter.ai/api/v1", Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: strPtrForOpenRouterTest("none")}}
	if req := buildRequestBody(optional, ctx, &goai.StreamOptions{}); req.Reasoning == nil || req.Reasoning["effort"] != "none" {
		t.Fatalf("optional default reasoning=%#v, want effort none", req.Reasoning)
	}
}
