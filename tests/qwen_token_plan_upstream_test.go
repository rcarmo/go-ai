package goai_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
)

// Source: pi v1.6.0 packages/ai/test/qwen-token-plan-models.test.ts:143,151,188,195,287.
func TestQwenTokenPlanModelsExposeTextAndOmitImageModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	// The current published catalogs replace qwen4-max-preview with qwen4-max in all four plans.
	team := []string{"MiniMax-M2.5", "deepseek-v4-pro", "glm-5", "glm-5.1", "glm-5.2", "kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code", "qwen3.6-flash", "qwen3.6-plus", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.8-max", "qwen4-max"}
	individual := []string{"deepseek-v4-flash-0731", "deepseek-v4-pro", "deepseek-v4-pro-0813", "deepseek-v4.1-flash", "deepseek-v4.1-flash-1017", "glm-5.2", "glm-5.3", "qwen3.6-flash", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.8-max", "qwen4-max"}
	slices.Sort(individual)
	imageModels := []string{"qwen-image-2.0", "qwen-image-2.0-pro", "wan2.7-image", "wan2.7-image-pro"}
	retired := []string{"qwen3.8-max-preview", "qwen4-max-preview", "deepseek-v3.2", "deepseek-v4-flash"}
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
				// The Individual endpoints come from the current published catalog.
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

// Source: pi v1.6.0 packages/ai/test/qwen-token-plan-models.test.ts:160-186.
func TestQwenTokenPlanEnvKeys(t *testing.T) {
	plans := []struct {
		provider goai.Provider
		planKey  string
		shared   string
	}{
		{goai.ProviderQwenTokenPlan, "QWEN_TOKEN_PLAN_TEAM_API_KEY", "QWEN_TOKEN_PLAN_API_KEY"},
		{goai.ProviderQwenTokenPlanCN, "QWEN_TOKEN_PLAN_TEAM_CN_API_KEY", "QWEN_TOKEN_PLAN_CN_API_KEY"},
		{goai.ProviderQwenTokenPlanIndividual, "QWEN_TOKEN_PLAN_INDIVIDUAL_API_KEY", "QWEN_TOKEN_PLAN_API_KEY"},
		{"qwen-token-plan-individual-cn", "QWEN_TOKEN_PLAN_INDIVIDUAL_CN_API_KEY", "QWEN_TOKEN_PLAN_CN_API_KEY"},
	}
	for _, p := range plans {
		t.Setenv(p.planKey, "")
		t.Setenv(p.shared, "")
	}
	check := func(t *testing.T, p goai.Provider, env goai.ProviderEnv, want string) {
		t.Helper()
		model := &goai.Model{Provider: p}
		if got := goai.GetEnvAPIKeyWithEnv(p, env); got != want {
			t.Errorf("scoped key = %q; want %q", got, want)
		}
		if got := goai.ResolveAPIKey(model, &goai.StreamOptions{Env: env}); got != want {
			t.Errorf("stream key = %q; want %q", got, want)
		}
	}
	for _, p := range plans {
		t.Run(string(p.provider), func(t *testing.T) {
			both := goai.ProviderEnv{p.planKey: "plan", p.shared: "shared"}
			check(t, p.provider, both, "plan")
			if got := goai.FindEnvKeys(p.provider, both); !slices.Equal(got, []string{p.planKey, p.shared}) {
				t.Fatalf("environment names = %q, want %q", got, []string{p.planKey, p.shared})
			}
			check(t, p.provider, goai.ProviderEnv{p.shared: "shared"}, "shared")
		})
	}
	t.Run("per-plan-key-isolation", func(t *testing.T) {
		individual := goai.ProviderEnv{"QWEN_TOKEN_PLAN_INDIVIDUAL_API_KEY": "test"}
		teamCN := goai.ProviderEnv{"QWEN_TOKEN_PLAN_TEAM_CN_API_KEY": "test"}
		if got := goai.FindEnvKeys(goai.ProviderQwenTokenPlan, individual); got != nil {
			t.Errorf("global team environment names = %q, want nil", got)
		}
		if got := goai.FindEnvKeys("qwen-token-plan-individual-cn", teamCN); got != nil {
			t.Errorf("China individual environment names = %q, want nil", got)
		}
		check(t, goai.ProviderQwenTokenPlan, individual, "")
		check(t, "qwen-token-plan-individual-cn", teamCN, "")
	})
}

// Source: pi v1.6.0 packages/ai/test/qwen-token-plan-models.test.ts:203-285,300-365.
func TestSourceV160QwenTokenPlanReasoning(t *testing.T) {
	teamThinking := []string{"deepseek-v4-pro", "glm-5", "glm-5.1", "glm-5.2", "kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code", "qwen3.6-flash", "qwen3.6-plus", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.8-max"}
	individualThinking := []string{"deepseek-v4-flash-0731", "deepseek-v4-pro", "deepseek-v4-pro-0813", "deepseek-v4.1-flash", "deepseek-v4.1-flash-1017", "glm-5.2", "glm-5.3", "qwen3.6-flash", "qwen3.7-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.8-max", "qwen4-max"}
	teamEffort := []string{"deepseek-v4-pro", "glm-5", "glm-5.1", "glm-5.2"}
	individualEffort := []string{"deepseek-v4-flash-0731", "deepseek-v4-pro", "deepseek-v4-pro-0813", "glm-5.2", "glm-5.3"}
	low, medium, high, xhigh, max := "low", "medium", "high", "xhigh", "max"
	for _, p := range []struct {
		provider         goai.Provider
		thinking, effort []string
	}{
		{goai.ProviderQwenTokenPlan, teamThinking, teamEffort},
		{goai.ProviderQwenTokenPlanCN, teamThinking, teamEffort},
		{goai.ProviderQwenTokenPlanIndividual, individualThinking, individualEffort},
		{"qwen-token-plan-individual-cn", individualThinking, individualEffort},
	} {
		t.Run(string(p.provider), func(t *testing.T) {
			// Source: openai-completions-tool-choice.test.ts:1227-1241.
			t.Run("qwen37-max-replay-metadata", func(t *testing.T) {
				model := requireModel(t, p.provider, "qwen3.7-max")
				compat := model.CompletionsCompat
				if compat == nil || compat.ThinkingFormat != "qwen" || compat.RequiresReasoningContentOnAssistantMessages != nil || compat.SupportsStore == nil || *compat.SupportsStore {
					t.Fatalf("qwen3.7-max compat=%+v; want qwen, no forced reasoning_content and explicit supportsStore=false", compat)
				}
			})
			for _, id := range p.thinking {
				t.Run("thinking-fields/"+id, func(t *testing.T) {
					payload := qwenStreamPayload(t, p.provider, id, goai.ThinkingHigh)
					assertCompletionsPayloadField(t, payload, "enable_thinking", "true")
					assertCompletionsPayloadField(t, payload, "thinking", "")
				})
			}
			if p.provider == goai.ProviderQwenTokenPlanIndividual || p.provider == "qwen-token-plan-individual-cn" {
				// The retired preview's high level was disabled; keep that wire case separate from the live successor.
				t.Run("historical-thinking-fields/qwen4-max-preview", func(t *testing.T) {
					preview := loadHistoricalCompletionsModel(t, "qwen-token-plan-preview-v160.json", 2, p.provider, "qwen4-max-preview")
					payload := captureCompletionsPayload(t, preview, goai.ThinkingHigh)
					assertCompletionsPayloadField(t, payload, "model", `"qwen4-max-preview"`)
					assertCompletionsPayloadField(t, payload, "enable_thinking", "true")
					assertCompletionsPayloadField(t, payload, "thinking", "")
				})
			}
			for _, id := range p.effort {
				t.Run("effort-map/"+id, func(t *testing.T) {
					want := map[goai.ModelThinkingLevel]*string{"minimal": nil, "low": nil, "medium": nil, "high": &high, "xhigh": nil, "max": &max}
					if id == "glm-5.3" {
						want["low"] = &low
					}
					assertQwenThinkingMap(t, p.provider, id, want)
				})
				t.Run("effort-payload/"+id, func(t *testing.T) {
					payload := qwenStreamPayload(t, p.provider, id, goai.ThinkingHigh)
					assertCompletionsPayloadField(t, payload, "reasoning_effort", `"high"`)
				})
			}
			for _, id := range []string{"qwen3.8-flash", "qwen3.8-max"} {
				t.Run("qwen38-effort-map/"+id, func(t *testing.T) {
					assertQwenThinkingMap(t, p.provider, id, map[goai.ModelThinkingLevel]*string{"minimal": nil, "low": &low, "medium": &medium, "high": nil, "xhigh": &xhigh, "max": nil})
				})
				t.Run("qwen38-effort-payload/"+id, func(t *testing.T) {
					payload := qwenStreamPayload(t, p.provider, id, goai.ThinkingXHigh)
					assertCompletionsPayloadField(t, payload, "enable_thinking", "true")
					assertCompletionsPayloadField(t, payload, "reasoning_effort", `"xhigh"`)
					assertCompletionsPayloadField(t, payload, "thinking", "")
				})
			}
		})
	}
}

func assertQwenThinkingMap(t *testing.T, provider goai.Provider, id string, want map[goai.ModelThinkingLevel]*string) {
	t.Helper()
	model := requireModel(t, provider, id)
	for level, expected := range want {
		got, ok := model.ThinkingLevelMap[level]
		if !ok || (got == nil) != (expected == nil) || ptrValue(got) != ptrValue(expected) {
			t.Errorf("%s = %q (present=%t, nil=%t); want %q (nil=%t)", level, ptrValue(got), ok, got == nil, ptrValue(expected), expected == nil)
		}
	}
}

// Load a detached, archived model without adding it to the live registry.
func loadHistoricalCompletionsModel(t *testing.T, file string, records int, provider goai.Provider, id string) goai.Model {
	t.Helper()
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		UpstreamCommit string `json:"upstreamCommit"`
		Models         []struct {
			goai.Model
			Compat *goai.OpenAICompletionsCompat `json:"compat"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.UpstreamCommit != "faac585884ae2c013a6b1400f3e9e77f10b34bac" || len(fixture.Models) != records {
		t.Fatalf("invalid v1.6.0 fixture %s", file)
	}
	for _, entry := range fixture.Models {
		if entry.Provider == provider && entry.ID == id && entry.Api == goai.ApiOpenAICompletions && entry.Compat != nil {
			entry.Model.CompletionsCompat = entry.Compat
			return entry.Model
		}
	}
	t.Fatalf("missing historical Completions record %s/%s in %s", provider, id, file)
	return goai.Model{}
}

func qwenStreamPayload(t *testing.T, provider goai.Provider, id string, effort goai.ThinkingLevel) map[string]json.RawMessage {
	t.Helper()
	return captureCompletionsPayload(t, *requireModel(t, provider, id), effort)
}

func captureCompletionsPayload(t *testing.T, model goai.Model, effort goai.ThinkingLevel) map[string]json.RawMessage {
	t.Helper()
	if model.Api != goai.ApiOpenAICompletions {
		t.Fatalf("unexpected API %q", model.Api)
	}
	// Keep the model's path and a loopback-only fallback if the fake transport is bypassed.
	baseURL, err := url.Parse(model.BaseURL)
	if err != nil || baseURL.Host == "" {
		t.Fatalf("invalid model base URL: %q", model.BaseURL)
	}
	model.BaseURL = "http://127.0.0.1:0" + baseURL.EscapedPath()
	adapter := goai.GetApiProvider(model.Api)
	if adapter == nil || adapter.StreamSimple == nil {
		t.Fatal("OpenAI Completions adapter not registered")
	}
	var hooks, requests, completed int
	var payload map[string]json.RawMessage
	opts := goai.StreamOptions{
		APIKey: "test", Reasoning: &effort,
		OnPayload: func(value any, _ *goai.Model) (any, error) {
			hooks++
			data, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			return nil, json.Unmarshal(data, &payload)
		},
		HTTPTransport: providerRetryTransportMain(func(req *http.Request) (*http.Response, error) {
			requests++
			mock := "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"prompt_tokens_details\":{\"cached_tokens\":0},\"completion_tokens_details\":{\"reasoning_tokens\":0}}}\n\ndata: [DONE]\n\n"
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(mock)), Request: req}, nil
		}),
	}
	var streamErr error
	for event := range adapter.StreamSimple(t.Context(), &model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hi")}}, &opts) {
		switch e := event.(type) {
		case *goai.ErrorEvent:
			streamErr = e.Err
		case *goai.DoneEvent:
			if e.Reason == goai.StopReasonStop {
				completed++
			}
		}
	}
	if hooks != 1 || requests != 1 || completed != 1 || streamErr != nil || payload == nil {
		t.Fatalf("stream hooks=%d requests=%d completed=%d; error=%v", hooks, requests, completed, streamErr)
	}
	return payload
}

func assertCompletionsPayloadField(t *testing.T, payload map[string]json.RawMessage, field, want string) {
	t.Helper()
	got, present := payload[field]
	if want == "" && present || want != "" && (!present || string(got) != want) {
		t.Errorf("%s = %s (present=%t); want %s (empty means absent)", field, got, present, want)
	}
}
