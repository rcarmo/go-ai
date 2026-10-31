package openairesponses

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"testing"
)

func TestResponsesReasoningCapabilitiesMain(t *testing.T) {
	high, off, empty := goai.ThinkingLevel("high"), goai.ThinkingLevel("off"), goai.ThinkingLevel("")
	cases := []struct {
		name    string
		level   *goai.ThinkingLevel
		summary string
	}{{"omitted", nil, ""}, {"high", &high, ""}, {"summary", nil, "concise"}, {"empty", &empty, ""}, {"off", &off, ""}, {"empty_summary", &empty, "concise"}}
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, provider := range []goai.Provider{goai.ProviderOpenAI, goai.ProviderXAI, goai.ProviderGitHubCopilot} {
			for _, reasoning := range []bool{false, true} {
				for _, unsupportedOff := range []bool{false, true} {
					for _, tc := range cases {
						t.Run(fmt.Sprintf("%s/%s/reasoning_%t/off_disabled_%t/%s", api, provider, reasoning, unsupportedOff, tc.name), func(t *testing.T) {
							model := responsesDeferredModel("gpt-5.4", provider)
							model.Api = api
							model.Reasoning = reasoning
							if unsupportedOff {
								model.ThinkingLevelMap = map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil}
							}
							opts := &goai.StreamOptions{Reasoning: tc.level, ReasoningSummary: tc.summary}
							beforeModel, _ := json.Marshal(model)
							beforeOptions, _ := json.Marshal(opts)
							request := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts)
							var want *reasoningConfig
							var include []string
							if reasoning {
								switch {
								case tc.name == "high":
									want = &reasoningConfig{Effort: "high", Summary: "auto"}
									include = []string{"reasoning.encrypted_content"}
								case tc.summary != "":
									want = &reasoningConfig{Effort: "medium", Summary: "concise"}
									include = []string{"reasoning.encrypted_content"}
								case !unsupportedOff && (api == goai.ApiAzureOpenAIResponses || provider != goai.ProviderGitHubCopilot):
									want = &reasoningConfig{Effort: "none"}
								}
								if api != goai.ApiAzureOpenAIResponses && provider == goai.ProviderXAI && include == nil {
									include = []string{"reasoning.encrypted_content"}
								}
							}
							if !reflect.DeepEqual(request.Reasoning, want) || !reflect.DeepEqual(request.Include, include) {
								t.Fatalf("reasoning=%#v include=%v want=%#v/%v", request.Reasoning, request.Include, want, include)
							}
							afterModel, _ := json.Marshal(model)
							afterOptions, _ := json.Marshal(opts)
							if string(beforeModel) != string(afterModel) || string(beforeOptions) != string(afterOptions) {
								t.Fatal("request projection mutated caller configuration")
							}
						})
					}
				}
			}
		}
	}
}

func TestResponsesSamplingLastWriterAcrossReasoningGateMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, reasoning := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reasoning_%t", api, reasoning), func(t *testing.T) {
				model := responsesDeferredModel("gpt-5.4", goai.ProviderOpenAI)
				model.Api = api
				model.Reasoning = reasoning
				model.SamplingParams = map[string]any{"temperature": .1, "top_p": .8}
				model.SamplingParamsByThinkingLevel = map[goai.ModelThinkingLevel]map[string]any{goai.ThinkingOff: {"top_k": float64(2)}, goai.ModelThinkingLevel("high"): {"top_k": float64(3)}}
				high := goai.ThinkingLevel("high")
				temperature := .7
				overrides := map[string]any{"temperature": nil, "reasoning": map[string]any{"effort": "custom"}, "include": []any{"custom.include"}, "max_output_tokens": float64(11), "service_tier": "custom-tier", "tool_choice": nil, "store": true, "stream": false}
				opts := &goai.StreamOptions{Reasoning: &high, Temperature: &temperature, SamplingParams: overrides}
				beforeModel, _ := json.Marshal(model)
				beforeOptions, _ := json.Marshal(opts)
				raw, err := json.Marshal(buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts))
				if err != nil {
					t.Fatal(err)
				}
				var body map[string]any
				if err = json.Unmarshal(raw, &body); err != nil {
					t.Fatal(err)
				}
				for key, want := range overrides {
					if value, ok := body[key]; !ok || !reflect.DeepEqual(value, want) {
						t.Fatal("sampling override lost", key, body[key], want)
					}
				}
				wantK := float64(2)
				if reasoning {
					wantK = 3
				}
				if body["top_p"] != .8 || body["top_k"] != wantK {
					t.Fatal("model/level sampling precedence", body)
				}
				afterModel, _ := json.Marshal(model)
				afterOptions, _ := json.Marshal(opts)
				if string(beforeModel) != string(afterModel) || string(beforeOptions) != string(afterOptions) {
					t.Fatal("sampling merge mutated caller configuration")
				}
			})
		}
	}
}
