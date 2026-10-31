package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestOpenRouterReasoningPayloadsMain(t *testing.T) {
	text := func(v string) *string { return &v }
	mandatory := map[goai.ModelThinkingLevel]*string{"off": nil, "minimal": nil, "low": text("low"), "medium": nil, "high": text("high"), "xhigh": nil, "max": text("max")}
	optional := map[goai.ModelThinkingLevel]*string{"off": text("none"), "minimal": nil, "low": text("low"), "medium": nil, "high": text("high"), "xhigh": nil, "max": nil}
	for _, tc := range []struct {
		name     string
		metadata map[goai.ModelThinkingLevel]*string
		wantOff  bool
	}{{"mandatory", mandatory, false}, {"mandatory-no-efforts", map[goai.ModelThinkingLevel]*string{"off": nil}, false}, {"optional", optional, true}, {"optional-no-metadata", nil, true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := samplingModelMain(goai.ApiOpenAICompletions)
			m.ID = "stealth/ox-alpha"
			m.Provider = goai.ProviderOpenRouter
			m.Reasoning = true
			m.ThinkingLevelMap = tc.metadata
			m.CompletionsCompat = &goai.OpenAICompletionsCompat{ThinkingFormat: "openrouter"}
			available := goai.GetSupportedThinkingLevels(m)
			hasOff := false
			for _, v := range available {
				if v == goai.ThinkingOff {
					hasOff = true
				}
			}
			if hasOff != tc.wantOff {
				t.Fatal("off metadata lost", available)
			}
			for level, mapped := range tc.metadata {
				present := false
				for _, selectable := range available {
					if selectable == level {
						present = true
					}
				}
				if present != (mapped != nil) {
					t.Fatalf("verified support lost for %s: %v", level, available)
				}
				if mapped != nil {
					effort, ok := goai.MapThinkingLevel(m, level)
					if !ok || effort != *mapped {
						t.Fatalf("effort %s=%q/%v, want %q", level, effort, ok, *mapped)
					}
				}
			}
			for _, state := range []string{"nil", "empty", "off", "low"} {
				t.Run(state, func(t *testing.T) {
					opts := goai.StreamOptions{}
					if state != "nil" {
						level := goai.ThinkingLevel(state)
						if state == "empty" {
							level = ""
						}
						opts.Reasoning = &level
					}
					p := captureSamplingMain(t, m, opts)
					want := ""
					if tc.wantOff {
						want = "none"
					}
					if state == "low" {
						want = "low"
					}
					actual, exists := p["reasoning"]
					if want == "" {
						if exists {
							t.Fatal("mandatory background effort injected", actual)
						}
						return
					}
					reasoning, ok := actual.(map[string]any)
					if !ok || reasoning["effort"] != want {
						t.Fatal("wrong effort payload", p, want)
					}
				})
			}
		})
	}
}

func TestNativeReasoningToggleControlsMain(t *testing.T) {
	for _, format := range []string{"zai", "qwen", "qwen-chat-template", "chat-template", "deepseek", "together"} {
		for _, state := range []goai.ThinkingLevel{"", goai.ThinkingLevel(goai.ThinkingOff), goai.ThinkingLow} {
			t.Run(format+"/"+string(state), func(t *testing.T) {
				m := samplingModelMain(goai.ApiOpenAICompletions)
				m.Reasoning = true
				yes := true
				m.CompletionsCompat = &goai.OpenAICompletionsCompat{ThinkingFormat: format, SupportsThinkingTokenBudget: &yes, ChatTemplateKwargs: map[string]goai.ChatTemplateKwargValue{"enable_thinking": {Var: "thinking.enabled"}}}
				p := captureSamplingMain(t, m, goai.StreamOptions{Reasoning: &state})
				enabled := state == goai.ThinkingLow
				if enabled {
					assertSamplingValuesMain(t, p, map[string]any{"thinking_token_budget": float64(2048)})
				} else {
					assertSamplingValuesMain(t, p, map[string]any{"thinking_token_budget": nil})
				}
				switch format {
				case "zai", "deepseek":
					thinking, ok := p["thinking"].(map[string]any)
					want := "disabled"
					if enabled {
						want = "enabled"
					}
					if !ok || thinking["type"] != want {
						t.Fatal("disabled thinking enabled", p)
					}
				case "qwen":
					if p["enable_thinking"] != enabled {
						t.Fatal("disabled thinking enabled", p)
					}
				case "together":
					reasoning, ok := p["reasoning"].(map[string]any)
					if !ok || reasoning["enabled"] != enabled {
						t.Fatal("disabled thinking enabled", p)
					}
				default:
					kwargs, ok := p["chat_template_kwargs"].(map[string]any)
					if !ok || kwargs["enable_thinking"] != enabled {
						t.Fatal("disabled thinking enabled", p)
					}
				}
			})
		}
	}
}
