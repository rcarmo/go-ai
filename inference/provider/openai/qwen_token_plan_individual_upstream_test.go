package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestQwenTokenPlanIndividualRequestFields(t *testing.T) {
	goai.RegisterBuiltinModels()

	for _, tc := range []struct {
		modelID            string
		reasoning          goai.ThinkingLevel
		wantEnable         bool
		wantEffort         string
		wantHasEffort      bool
		wantThinkingLevels map[goai.ModelThinkingLevel]*string
	}{
		{
			modelID:       "deepseek-v4-pro",
			reasoning:     goai.ThinkingHigh,
			wantEnable:    true,
			wantEffort:    "high",
			wantHasEffort: true,
			wantThinkingLevels: map[goai.ModelThinkingLevel]*string{
				goai.ThinkingOff: nil,
				goai.ModelThinkingLevel(goai.ThinkingMinimal): nil,
				goai.ModelThinkingLevel(goai.ThinkingLow):     nil,
				goai.ModelThinkingLevel(goai.ThinkingMedium):  nil,
				goai.ModelThinkingLevel(goai.ThinkingHigh):    strPtrTest("high"),
				goai.ModelThinkingLevel(goai.ThinkingXHigh):   nil,
				goai.ModelThinkingLevel(goai.ThinkingMax):     strPtrTest("max"),
			},
		},
		{
			modelID:       "qwen3.8-max",
			reasoning:     goai.ThinkingXHigh,
			wantEnable:    true,
			wantEffort:    "xhigh",
			wantHasEffort: true,
			wantThinkingLevels: map[goai.ModelThinkingLevel]*string{
				goai.ThinkingOff: nil,
				goai.ModelThinkingLevel(goai.ThinkingMinimal): nil,
				goai.ModelThinkingLevel(goai.ThinkingLow):     strPtrTest("low"),
				goai.ModelThinkingLevel(goai.ThinkingMedium):  strPtrTest("medium"),
				goai.ModelThinkingLevel(goai.ThinkingHigh):    nil,
				goai.ModelThinkingLevel(goai.ThinkingXHigh):   strPtrTest("xhigh"),
				goai.ModelThinkingLevel(goai.ThinkingMax):     nil,
			},
		},
		{
			modelID:       "qwen3.6-flash",
			reasoning:     goai.ThinkingHigh,
			wantEnable:    true,
			wantHasEffort: false,
		},
	} {
		t.Run(tc.modelID, func(t *testing.T) {
			model := goai.GetModel(goai.ProviderQwenTokenPlanIndividual, tc.modelID)
			if model == nil {
				t.Fatalf("missing qwen-token-plan-individual/%s", tc.modelID)
			}
			// Published v1.6.1 qwen-token-plan-individual.json; absence of the
			// Flash map is distinct from the explicit nulls on the other models.
			if model.Api != goai.ApiOpenAICompletions || model.BaseURL != "https://token-plan.maas.qwencloudapi.com/compatible-mode/v1" {
				t.Fatalf("unexpected Individual metadata: api=%q baseURL=%q", model.Api, model.BaseURL)
			}
			strict, store := true, false
			wantCompat := &goai.OpenAICompletionsCompat{ThinkingFormat: "qwen", SupportsStrictMode: &strict,
				SupportsStore: &store, SupportsReasoningEffort: &tc.wantHasEffort}
			if !reflect.DeepEqual(model.CompletionsCompat, wantCompat) {
				t.Fatalf("qwen compat=%#v, want %#v", model.CompletionsCompat, wantCompat)
			}
			if !reflect.DeepEqual(model.ThinkingLevelMap, tc.wantThinkingLevels) {
				t.Fatalf("thinking levels=%#v, want %#v", model.ThinkingLevelMap, tc.wantThinkingLevels)
			}
			req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hi")}}, &goai.StreamOptions{APIKey: "test", Reasoning: &tc.reasoning})
			payload := marshalChatRequestMap(t, req)
			if got := payload["enable_thinking"]; got != tc.wantEnable {
				t.Fatalf("enable_thinking=%#v, want %#v; payload=%#v", got, tc.wantEnable, payload)
			}
			gotEffort, hasEffort := payload["reasoning_effort"]
			if hasEffort != tc.wantHasEffort || (hasEffort && gotEffort != tc.wantEffort) {
				t.Fatalf("reasoning_effort=%#v has=%v, want %q has=%v; payload=%#v", gotEffort, hasEffort, tc.wantEffort, tc.wantHasEffort, payload)
			}
			if _, ok := payload["thinking"]; ok {
				t.Fatalf("qwen request should not include thinking object: %#v", payload)
			}
		})
	}
}

func TestQwenTokenPlanIndividualNoToolHistoryOmitsEmptyTools(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderQwenTokenPlanIndividual, "qwen3.8-max")
	if model == nil {
		t.Fatal("missing Individual qwen3.8-max")
	}
	reasoning := goai.ThinkingXHigh
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hi")}, Tools: []goai.Tool{}}, &goai.StreamOptions{APIKey: "test", Reasoning: &reasoning})
	payload := marshalChatRequestMap(t, req)
	if _, ok := payload["tools"]; ok {
		t.Fatalf("empty tools without tool history should be omitted: %#v", payload)
	}
}

func marshalChatRequestMap(t *testing.T, req chatRequest) map[string]any {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v; body=%s", err, body)
	}
	return payload
}

func strPtrTest(v string) *string { return &v }
