package goai_test

import (
	"slices"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 packages/ai/test/qwen-token-plan-models.test.ts:143,151,188,195,287.
func TestQwenTokenPlanModelsExposeTextAndOmitImageModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	team := []string{"MiniMax-M2.5", "deepseek-v4-pro", "glm-5", "glm-5.1", "glm-5.2", "kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code", "qwen3.6-flash", "qwen3.6-plus", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.8-max"}
	individual := []string{"deepseek-v4-flash-0731", "deepseek-v4-pro", "deepseek-v4-pro-0813", "deepseek-v4.1-flash", "deepseek-v4.1-flash-1017", "glm-5.2", "glm-5.3", "qwen3.6-flash", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.8-max", "qwen4-max-preview"}
	slices.Sort(individual)
	imageModels := []string{"qwen-image-2.0", "qwen-image-2.0-pro", "wan2.7-image", "wan2.7-image-pro"}
	retired := []string{"qwen3.8-max-preview", "deepseek-v3.2", "deepseek-v4-flash"}
	for _, tc := range []struct {
		provider      goai.Provider
		individualURL string
	}{
		{goai.ProviderQwenTokenPlan, ""},
		{goai.ProviderQwenTokenPlanCN, ""},
		{goai.ProviderQwenTokenPlanIndividual, "https://token-plan.maas.qwencloudapi.com/compatible-mode/v1"},
		{"qwen-token-plan-individual-cn", "https://token-plan.maas.qianwenaiapi.com/compatible-mode/v1"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			models := goai.ListModels(tc.provider)
			ids := make([]string, 0, len(models))
			for _, model := range models {
				ids = append(ids, model.ID)
				// Preserve the existing supplemental catalog metadata checks. The
				// Individual URLs come from the exact v1.6.0 archive records.
				if model.Api != goai.ApiOpenAICompletions || model.Provider != tc.provider || tc.individualURL != "" && model.BaseURL != tc.individualURL {
					t.Fatalf("unexpected %s model route: %s %s %s", tc.provider, model.ID, model.Api, model.BaseURL)
				}
			}
			if tc.individualURL != "" {
				slices.Sort(ids)
				if !slices.Equal(ids, individual) {
					t.Fatalf("%s model IDs = %v; want exact %v", tc.provider, ids, individual)
				}
			} else {
				for _, id := range team {
					if !slices.Contains(ids, id) {
						t.Errorf("%s should include %s", tc.provider, id)
					}
				}
				// Existing supplemental check: the dated successor is still registered.
				if !slices.Contains(ids, "deepseek-v4-flash-0731") {
					t.Errorf("%s should include the dated Flash model", tc.provider)
				}
			}
			for _, id := range imageModels {
				if slices.Contains(ids, id) {
					t.Errorf("%s should omit image model %s", tc.provider, id)
				}
			}
			for _, id := range retired {
				if slices.Contains(ids, id) {
					t.Errorf("%s should omit retired model %s", tc.provider, id)
				}
			}
		})
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
