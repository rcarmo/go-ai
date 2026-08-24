package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0843ZAICodingPlanModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	global := map[string]bool{}
	for _, model := range goai.ListModels(goai.ProviderZAI) {
		global[model.ID] = true
		if model.BaseURL != "https://api.z.ai/api/coding/paas/v4" || model.Api != goai.ApiOpenAICompletions {
			t.Fatalf("unexpected global ZAI coding model: %#v", model)
		}
	}
	for _, id := range []string{"glm-4.7", "glm-5-turbo", "glm-5.2", "glm-5.2-highspeed", "glm-5.3"} {
		if !global[id] {
			t.Fatalf("global zai should include %s", id)
		}
	}
	cn := map[string]bool{}
	for _, model := range goai.ListModels(goai.ProviderZAICodingCN) {
		cn[model.ID] = true
		if model.BaseURL != "https://open.bigmodel.cn/api/coding/paas/v4" || model.Api != goai.ApiOpenAICompletions {
			t.Fatalf("unexpected CN ZAI coding model: %#v", model)
		}
	}
	for _, id := range []string{"glm-4.6v", "glm-4.7", "glm-5-turbo", "glm-5.1", "glm-5.2", "glm-5.2-highspeed", "glm-5.3", "glm-5v-turbo"} {
		if !cn[id] {
			t.Fatalf("zai-coding-cn should include %s", id)
		}
	}
}

func TestV0843ZAICodingPlanEnvKeys(t *testing.T) {
	env := goai.ProviderEnv{"ZAI_API_KEY": "global", "ZAI_CODING_CN_API_KEY": "cn"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderZAI, env); got != "global" {
		t.Fatalf("zai env key=%q", got)
	}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderZAICodingCN, env); got != "cn" {
		t.Fatalf("zai coding cn env key=%q", got)
	}
}
