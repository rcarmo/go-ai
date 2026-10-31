package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestPinnedThinkingBudgetPayloadsMain(t *testing.T) {
	yes := true
	integer := func(v int) *int { return &v }
	level := func(v goai.ThinkingLevel) *goai.ThinkingLevel { return &v }
	for _, tc := range []struct {
		name    string
		compat  goai.OpenAICompletionsCompat
		level   *goai.ThinkingLevel
		budgets *goai.ThinkingBudgets
		ceiling *int
		key     string
		want    any
	}{
		{name: "configured", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: &yes}, level: level(goai.ThinkingMedium), budgets: &goai.ThinkingBudgets{Medium: integer(4096)}, key: "thinking_token_budget", want: float64(4096)},
		{name: "unconfigured", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai"}, level: level(goai.ThinkingMedium), budgets: &goai.ThinkingBudgets{Medium: integer(4096)}, key: "thinking_token_budget"},
		{name: "off", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: &yes}, budgets: &goai.ThinkingBudgets{High: integer(8192)}, key: "thinking_token_budget"},
		{name: "xhigh", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: &yes}, level: level(goai.ThinkingXHigh), budgets: &goai.ThinkingBudgets{High: integer(8192)}, key: "thinking_token_budget", want: float64(8192)},
		{name: "max", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: &yes}, level: level(goai.ThinkingMax), budgets: &goai.ThinkingBudgets{High: integer(8192)}, key: "thinking_token_budget", want: float64(8192)},
		{name: "answer-room", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: &yes}, level: level(goai.ThinkingHigh), key: "thinking_token_budget", want: float64(15360)},
		{name: "caller-ceiling", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai", SupportsThinkingTokenBudget: &yes}, level: level(goai.ThinkingHigh), budgets: &goai.ThinkingBudgets{High: integer(8192)}, ceiling: integer(4096), key: "thinking_token_budget", want: float64(3072)},
		{name: "budget-field", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "qwen", ThinkingTokenBudgetField: "thinking_budget"}, level: level(goai.ThinkingMedium), budgets: &goai.ThinkingBudgets{Medium: integer(4096)}, key: "thinking_budget", want: float64(4096)},
		{name: "budget-tokens-field", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "qwen", ThinkingTokenBudgetField: "thinking_budget_tokens"}, level: level(goai.ThinkingMedium), budgets: &goai.ThinkingBudgets{Medium: integer(4096)}, key: "thinking_budget_tokens", want: float64(4096)},
		{name: "field-wins-alias", compat: goai.OpenAICompletionsCompat{ThinkingFormat: "zai", ThinkingTokenBudgetField: "thinking_budget", SupportsThinkingTokenBudget: &yes}, level: level(goai.ThinkingMedium), budgets: &goai.ThinkingBudgets{Medium: integer(4096)}, key: "thinking_budget", want: float64(4096)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := samplingModelMain(goai.ApiOpenAICompletions)
			m.ID = "zai-org/glm-5.2"
			m.Provider = "local-vllm"
			m.ContextWindow = 262144
			m.Reasoning = true
			m.CompletionsCompat = &tc.compat
			p := captureSamplingMain(t, m, goai.StreamOptions{Reasoning: tc.level, ThinkingBudgets: tc.budgets, MaxTokens: tc.ceiling})
			assertSamplingValuesMain(t, p, map[string]any{tc.key: tc.want})
			for _, key := range []string{"thinking_token_budget", "thinking_budget", "thinking_budget_tokens"} {
				if key != tc.key {
					if _, exists := p[key]; exists {
						t.Fatal("budget leaked into wrong field", key, p)
					}
				}
			}
		})
	}
}
func TestPinnedChatTemplateBudgetMain(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			m := samplingModelMain(goai.ApiOpenAICompletions)
			m.Reasoning = true
			m.ContextWindow = 262144
			m.CompletionsCompat = &goai.OpenAICompletionsCompat{ThinkingFormat: "chat-template", ChatTemplateKwargs: map[string]goai.ChatTemplateKwargValue{"enable_thinking": {Var: "thinking.enabled"}, "thinking_budget": {Var: "thinking.budget"}}}
			opts := goai.StreamOptions{}
			if enabled {
				high := goai.ThinkingHigh
				opts.Reasoning = &high
			}
			p := captureSamplingMain(t, m, opts)
			kwargs, ok := p["chat_template_kwargs"].(map[string]any)
			if !ok {
				t.Fatal("missing kwargs", p)
			}
			assertSamplingValuesMain(t, kwargs, map[string]any{"enable_thinking": enabled})
			if enabled {
				assertSamplingValuesMain(t, kwargs, map[string]any{"thinking_budget": float64(15360)})
			} else {
				assertSamplingValuesMain(t, kwargs, map[string]any{"thinking_budget": nil})
			}
			if _, exists := p["thinking_token_budget"]; exists {
				t.Fatal("template budget duplicated as top-level field", p)
			}
		})
	}
}

func TestChatTemplateUnavailableBudgetOmittedMain(t *testing.T) {
	for _, omit := range []bool{false, true} {
		for _, tc := range []struct {
			name   string
			level  goai.ThinkingLevel
			cap    int
			budget *int
		}{{name: "explicit-off", level: goai.ThinkingLevel(goai.ThinkingOff)}, {name: "empty"}, {name: "tiny-ceiling", level: goai.ThinkingHigh, cap: 512}, {name: "zero-budget", level: goai.ThinkingHigh, budget: new(int)}} {
			t.Run(tc.name+map[bool]string{false: "/default", true: "/omit"}[omit], func(t *testing.T) {
				m := samplingModelMain(goai.ApiOpenAICompletions)
				m.Reasoning = true
				m.ContextWindow = 262144
				m.CompletionsCompat = &goai.OpenAICompletionsCompat{ThinkingFormat: "chat-template", ChatTemplateKwargs: map[string]goai.ChatTemplateKwargValue{"enable_thinking": {Var: "thinking.enabled"}, "thinking_budget": {Var: "thinking.budget", OmitWhenOff: omit}}}
				opts := goai.StreamOptions{Reasoning: &tc.level}
				if tc.cap > 0 {
					opts.MaxTokens = &tc.cap
				}
				if tc.budget != nil {
					opts.ThinkingBudgets = &goai.ThinkingBudgets{High: tc.budget}
				}
				p := captureSamplingMain(t, m, opts)
				kwargs, ok := p["chat_template_kwargs"].(map[string]any)
				if !ok {
					t.Fatal("missing kwargs", p)
				}
				assertSamplingValuesMain(t, kwargs, map[string]any{"thinking_budget": nil})
			})
		}
	}
}
