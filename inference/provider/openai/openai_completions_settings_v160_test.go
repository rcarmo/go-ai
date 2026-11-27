package openai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

func localCompletionsModelV160(id, name string) goai.Model {
	return goai.Model{ID: id, Name: name, Api: goai.ApiOpenAICompletions, Provider: "local-vllm", BaseURL: "http://localhost:8000/v1", Reasoning: true, Input: []string{"text"}, ContextWindow: 128_000, MaxTokens: 8192}
}

func assertCompletionsValueV160(t *testing.T, got map[string]any, field string, want any) {
	t.Helper()
	value, present := got[field]
	if present != (want != nil) || (present && !reflect.DeepEqual(value, want)) {
		t.Errorf("%s = %#v (present=%t), want %#v", field, value, present, want)
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1416–1507.
func TestSourceV160CompletionsKimiThinkingSettings(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	conv := completionsChoiceContextV160("Hi")
	for _, tc := range []struct {
		provider            goai.Provider
		id, label, thinking string
		level               goai.ThinkingLevel
	}{
		{goai.ProviderOpenCode, "kimi-k2.6", "off", "disabled", ""},
		{goai.ProviderOpenCode, "kimi-k2.6", "high", "enabled", goai.ThinkingHigh},
		{goai.ProviderMoonshotAI, "kimi-k2.7-code", "off", "", ""},
		{goai.ProviderMoonshotAICN, "kimi-k2.7-code", "off", "", ""},
		{goai.ProviderMoonshotAICN, "kimi-k2.6", "off", "disabled", ""},
	} {
		t.Run(string(tc.provider)+"/"+tc.id+"/"+tc.label, func(t *testing.T) {
			model := completionsCatalogueWitnessV160(t, tc.provider, tc.id)
			got := captureCompletionsEffortV160(t, model, conv, tc.level)
			var want any
			if tc.thinking != "" {
				want = map[string]any{"type": tc.thinking}
			}
			assertCompletionsValueV160(t, got.payload, "thinking", want)
			got.field(t, "reasoning_effort", "")
		})
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1607–1627.
// Despite the suite name, published Grok Build selects the Responses adapter.
func TestSourceV160OpenCodeGrokBuildOmitsReasoningEffort(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheCatalogueV160(t, goai.ProviderOpenCode, "grok-build-0.1")
	if model.Api != goai.ApiOpenAIResponses {
		t.Fatalf("Grok Build API=%s, want Responses", model.Api)
	}
	var payload map[string]any
	requests := 0
	transport := openaiTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		defer r.Body.Close()
		if want := strings.TrimRight(model.BaseURL, "/") + "/responses"; r.Method != http.MethodPost || r.URL.String() != want {
			return nil, fmt.Errorf("expected POST %s, got %s %s", want, r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return successfulSSETestResponse(r, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"), nil
	})
	level := goai.ThinkingHigh
	got, err := goai.Complete(t.Context(), model, completionsChoiceContextV160("Hi"), &goai.StreamOptions{APIKey: "test", Reasoning: &level, HTTPTransport: transport})
	if err != nil || got == nil || requests != 1 || payload == nil {
		t.Fatalf("Responses capture: result=%v requests=%d payload=%#v err=%v", got, requests, payload, err)
	}
	assertCompletionsValueV160(t, payload, "reasoning_effort", nil)
}

func assertCompletionsMaxTokensV160(t *testing.T, model *goai.Model, catalog bool) {
	t.Helper()
	if catalog && (model.CompletionsCompat == nil || model.CompletionsCompat.MaxTokensField != "max_tokens") {
		t.Fatalf("compat=%+v, want declared maxTokensField=max_tokens", model.CompletionsCompat)
	}
	level, limit := goai.ThinkingLevel(""), 123
	got := captureCompletionsWireV160(t, model, completionsChoiceContextV160("Hi"), goai.StreamOptions{MaxTokens: &limit, Reasoning: &level})
	assertCompletionsValueV160(t, got.payload, "max_tokens", float64(123))
	got.field(t, "max_completion_tokens", "")
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1509–1534,1580–1605.
// The DeepSeek matrix is covered by TestV0842DeepSeekMixedCaseURLUsesMaxTokensField.
func TestSourceV160CompletionsOpenCodeAndZAIMaxTokens(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	for _, tc := range []struct {
		provider goai.Provider
		id       string
	}{
		{goai.ProviderOpenCodeGo, "kimi-k3"}, {goai.ProviderOpenCode, "kimi-k2.6"},
		{goai.ProviderZAI, "glm-5-turbo"}, {goai.ProviderZAI, "glm-5.2"},
	} {
		t.Run(string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			assertCompletionsMaxTokensV160(t, completionsCatalogueWitnessV160(t, tc.provider, tc.id), true)
		})
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1797–1898.
func TestSourceV160CompletionsReasoningFormats(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	conv := completionsChoiceContextV160("Hi")
	t.Run("OpenRouter reasoning object", func(t *testing.T) {
		model := promptCacheCatalogueV160(t, goai.ProviderOpenRouter, "deepseek/deepseek-r1-0528")
		got := captureCompletionsEffortV160(t, model, conv, goai.ThinkingHigh)
		assertCompletionsValueV160(t, got.payload, "reasoning", map[string]any{"effort": "high"})
		got.field(t, "reasoning_effort", "")
	})
	for _, tc := range []struct {
		label, id, name, compatJSON string
		level                       goai.ThinkingLevel
		want                        map[string]any
		absentThinking              bool
	}{
		{"boolean high", "deepseek-ai/DeepSeek-V3.1", "DeepSeek V3.1 via vLLM", `{"thinkingFormat":"chat-template","supportsReasoningEffort":false,"chatTemplateKwargs":{"thinking":{"$var":"thinking.enabled"}}}`, goai.ThinkingHigh, map[string]any{"thinking": true}, true},
		{"boolean off", "deepseek-ai/DeepSeek-V3.1", "DeepSeek V3.1 via vLLM", `{"thinkingFormat":"chat-template","supportsReasoningEffort":false,"chatTemplateKwargs":{"thinking":{"$var":"thinking.enabled"}}}`, "", map[string]any{"thinking": false}, true},
		{"qwen high", "Qwen/Qwen3-Coder", "Qwen3 Coder via vLLM", `{"thinkingFormat":"qwen-chat-template","supportsReasoningEffort":false}`, goai.ThinkingHigh, map[string]any{"enable_thinking": true, "preserve_thinking": true}, false},
		{"qwen off", "Qwen/Qwen3-Coder", "Qwen3 Coder via vLLM", `{"thinkingFormat":"qwen-chat-template","supportsReasoningEffort":false}`, "", map[string]any{"enable_thinking": false, "preserve_thinking": true}, false},
		{"effort with static kwargs", "unsloth/gpt-oss-120b-GGUF", "GPT OSS via vLLM", `{"thinkingFormat":"chat-template","supportsReasoningEffort":false,"chatTemplateKwargs":{"reasoning_effort":{"$var":"thinking.effort","omitWhenOff":true}}}`, goai.ThinkingXHigh, map[string]any{"preserve_thinking": true, "reasoning_effort": "max"}, false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			model := localCompletionsModelV160(tc.id, tc.name)
			if err := json.Unmarshal([]byte(tc.compatJSON), &model.CompletionsCompat); err != nil {
				t.Fatal(err)
			}
			if tc.level == goai.ThinkingXHigh {
				// The Go model stores a literal separately from variable references.
				model.CompletionsCompat.ChatTemplateKwargs["preserve_thinking"] = goai.ChatTemplateKwargValue{Value: true}
				maximum := "max"
				model.ThinkingLevelMap = map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingXHigh): &maximum}
			}
			got := captureCompletionsEffortV160(t, &model, conv, tc.level)
			assertCompletionsValueV160(t, got.payload, "chat_template_kwargs", tc.want)
			if tc.absentThinking {
				got.field(t, "thinking", "")
			}
			got.field(t, "reasoning_effort", "")
		})
	}
}

func captureAntLing(t *testing.T, model *goai.Model, ctx *goai.Context, opts goai.StreamOptions, direct bool) promptCacheRequestV160 {
	t.Helper()
	adapter := goai.GetApiProvider(model.Api)
	if adapter == nil {
		t.Fatal("missing Completions adapter")
	}
	stream := adapter.StreamSimple
	if direct {
		stream = adapter.Stream
	}
	if stream == nil {
		t.Fatal("missing selected Completions route")
	}
	return captureCompletionsWireUsingV160(t, model, ctx, opts, stream)
}

// Source: pi v1.7.0 openai-completions-tool-choice.test.ts:1828–1901.
func TestCompletionsAntLingThinkingCompatibility(t *testing.T) {
	t.Setenv("PI_CACHE_RETENTION", "")
	goai.RegisterBuiltinModels()
	model := promptCacheCatalogueV160(t, goai.ProviderAntLing, "Ling-3.1-flash")
	hi := completionsChoiceContextV160("Hi")
	high := goai.ThinkingHigh
	t.Run("metadata-and-wire", func(t *testing.T) {
		compat := model.CompletionsCompat
		if compat == nil {
			t.Fatal("missing Ant Ling catalogue compatibility")
		}
		if compat.SupportsStore == nil || *compat.SupportsStore || compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort ||
			compat.MaxTokensField != "max_tokens" || compat.ThinkingFormat != "deepseek" || compat.SupportsStrictMode == nil || !*compat.SupportsStrictMode ||
			compat.SupportsLongCacheRetention == nil || *compat.SupportsLongCacheRetention {
			t.Errorf("published Ant Ling compatibility = %+v", compat)
		}
		limit := 123
		ctx := &goai.Context{SystemPrompt: "Follow instructions.", Messages: hi.Messages}
		got := captureAntLing(t, model, ctx, goai.StreamOptions{Reasoning: &high, MaxTokens: &limit, CacheRetention: goai.CacheRetentionLong, SessionID: "ant-ling-session"}, false)
		assertCompletionsValueV160(t, got.payload, "max_tokens", float64(123))
		got.field(t, "max_completion_tokens", "")
		messages, ok := got.payload["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatalf("missing Ant Ling messages: %#v", got.payload["messages"])
		}
		first, _ := messages[0].(map[string]any)
		if first["role"] != "system" {
			t.Errorf("Ant Ling first role = %#v, want system", first)
		}
		assertCompletionsValueV160(t, got.payload, "thinking", map[string]any{"type": "enabled"})
		got.field(t, "reasoning_effort", "")
		got.field(t, "store", "")
		got.field(t, "prompt_cache_key", "")
		got.field(t, "prompt_cache_retention", "")
	})

	t.Run("legacy-format-alias", func(t *testing.T) {
		if model.CompletionsCompat == nil {
			t.Fatal("missing published compatibility to clone")
		}
		legacy := *model
		compat := *model.CompletionsCompat
		compat.ThinkingFormat = "ant-ling"
		legacy.CompletionsCompat = &compat
		if got := goai.DetectCompatForModel(&legacy).ThinkingFormat; got != "deepseek" {
			t.Errorf("normalized legacy Ant Ling format = %q, want deepseek", got)
		}
		for _, row := range []struct {
			name  string
			level *goai.ThinkingLevel
		}{{"off", nil}, {"high", &high}} {
			t.Run(row.name, func(t *testing.T) {
				opts := goai.StreamOptions{Reasoning: row.level}
				want := captureAntLing(t, model, hi, opts, false).payload
				if got := captureAntLing(t, &legacy, hi, opts, false).payload; !reflect.DeepEqual(got, want) {
					t.Errorf("deprecated Ant Ling payload = %#v, current = %#v", got, want)
				}
			})
		}
	})

	t.Run("thinking-levels", func(t *testing.T) {
		if got := goai.GetSupportedThinkingLevels(model); !slices.Equal(got, []goai.ModelThinkingLevel{"off", "high"}) {
			t.Errorf("Ant Ling selectable levels = %v, want [off high]", got)
		}
		for _, direct := range []bool{false, true} {
			route := "simple"
			if direct {
				route = "direct"
			}
			for _, level := range []goai.ThinkingLevel{"minimal", "low", "medium", "high", "xhigh"} {
				t.Run(route+"/"+string(level), func(t *testing.T) {
					got := captureAntLing(t, model, hi, goai.StreamOptions{Reasoning: &level}, direct)
					assertCompletionsValueV160(t, got.payload, "thinking", map[string]any{"type": "enabled"})
					got.field(t, "reasoning_effort", "")
				})
			}
		}
		off := captureAntLing(t, model, hi, goai.StreamOptions{}, false)
		assertCompletionsValueV160(t, off.payload, "thinking", map[string]any{"type": "disabled"})
		off.field(t, "reasoning_effort", "")
	})

	t.Run("inferred-format", func(t *testing.T) {
		inferred := &goai.Model{ID: "ant-ling-fixture", Provider: goai.ProviderAntLing, Api: goai.ApiOpenAICompletions,
			BaseURL: "https://api.ant-ling.com/v1", Input: []string{"text"}, Reasoning: true, ContextWindow: 100_000, MaxTokens: 1024}
		if got := goai.DetectCompatForModel(inferred).ThinkingFormat; got != "deepseek" {
			t.Errorf("inferred Ant Ling format = %q, want deepseek", got)
		}
		unmapped := *inferred
		unmapped.ThinkingLevelMap = map[goai.ModelThinkingLevel]*string{
			"off": nil, "minimal": nil, "low": nil, "medium": nil, "high": nil, "xhigh": nil, "max": nil,
		}
		for _, direct := range []bool{false, true} {
			for _, model := range []*goai.Model{inferred, &unmapped} {
				got := captureAntLing(t, model, hi, goai.StreamOptions{Reasoning: &high}, direct)
				assertCompletionsValueV160(t, got.payload, "thinking", map[string]any{"type": "enabled"})
				got.field(t, "reasoning", "")
				got.field(t, "reasoning_effort", "")
			}
			off := captureAntLing(t, inferred, hi, goai.StreamOptions{}, direct)
			assertCompletionsValueV160(t, off.payload, "thinking", map[string]any{"type": "disabled"})
		}
	})
}
