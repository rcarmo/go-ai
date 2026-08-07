package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestQwenTokenPlanModelsExposeTextAndOmitImageModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	textModels := []string{"MiniMax-M2.5", "deepseek-v3.2", "deepseek-v4-flash", "deepseek-v4-flash-0731", "deepseek-v4-pro", "glm-5", "glm-5.1", "glm-5.2", "kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code", "qwen3.6-flash", "qwen3.6-plus", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-max"}
	individualModels := []string{"deepseek-v4-flash-0731", "deepseek-v4-pro", "glm-5.2", "qwen3.6-flash", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-max"}
	imageModels := []string{"qwen-image-2.0", "qwen-image-2.0-pro", "wan2.7-image", "wan2.7-image-pro"}
	for _, provider := range []goai.Provider{goai.ProviderQwenTokenPlan, goai.ProviderQwenTokenPlanCN} {
		ids := map[string]bool{}
		for _, model := range goai.ListModels(provider) {
			ids[model.ID] = true
			if model.Api != goai.ApiOpenAICompletions || model.Provider != provider {
				t.Fatalf("unexpected %s model metadata: %#v", provider, model)
			}
		}
		for _, id := range textModels {
			if !ids[id] {
				t.Fatalf("%s should include %s", provider, id)
			}
		}
		for _, id := range imageModels {
			if ids[id] {
				t.Fatalf("%s should omit image model %s", provider, id)
			}
		}
	}

	individualIDs := map[string]bool{}
	for _, model := range goai.ListModels(goai.ProviderQwenTokenPlanIndividual) {
		individualIDs[model.ID] = true
		if model.Api != goai.ApiOpenAICompletions || model.Provider != goai.ProviderQwenTokenPlanIndividual || model.BaseURL != "https://token-plan.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1" {
			t.Fatalf("unexpected qwen-token-plan-individual model metadata: %#v", model)
		}
	}
	if len(individualIDs) != len(individualModels) {
		t.Fatalf("qwen-token-plan-individual models=%v, want exactly %v", individualIDs, individualModels)
	}
	for _, id := range individualModels {
		if !individualIDs[id] {
			t.Fatalf("qwen-token-plan-individual should include %s", id)
		}
	}
	for _, id := range append(imageModels, "qwen3.8-max-preview") {
		if individualIDs[id] {
			t.Fatalf("qwen-token-plan-individual should omit %s", id)
		}
	}
}

func TestQwenTokenPlanEnvKeys(t *testing.T) {
	env := goai.ProviderEnv{"QWEN_TOKEN_PLAN_API_KEY": "global", "QWEN_TOKEN_PLAN_CN_API_KEY": "cn"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderQwenTokenPlan, env); got != "global" {
		t.Fatalf("qwen token plan key=%q", got)
	}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderQwenTokenPlanCN, env); got != "cn" {
		t.Fatalf("qwen token plan cn key=%q", got)
	}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderQwenTokenPlanIndividual, env); got != "global" {
		t.Fatalf("qwen token plan individual key=%q", got)
	}
}
