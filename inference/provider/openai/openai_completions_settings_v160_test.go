package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
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
	transport := promptCacheTransportV160(func(r *http.Request) (*http.Response, error) {
		requests++
		defer r.Body.Close()
		if want := strings.TrimRight(model.BaseURL, "/") + "/responses"; r.Method != http.MethodPost || r.URL.String() != want {
			return nil, fmt.Errorf("expected POST %s, got %s %s", want, r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")), Request: r}, nil
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

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1900–1988.
func TestSourceV160CompletionsAntLingSettings(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	ring := promptCacheCatalogueV160(t, "ant-ling", "Ring-2.6-1T")
	hi := completionsChoiceContextV160("Hi")
	t.Run("catalogue and simple high", func(t *testing.T) {
		compat := ring.CompletionsCompat
		if compat == nil {
			t.Fatal("missing Ant Ling compat")
		}
		for name, value := range map[string]*bool{"supportsStore": compat.SupportsStore, "supportsReasoningEffort": compat.SupportsReasoningEffort, "supportsLongCacheRetention": compat.SupportsLongCacheRetention} {
			if value == nil || *value {
				t.Errorf("%s must be explicitly false", name)
			}
		}
		if compat.MaxTokensField != "max_tokens" || compat.ThinkingFormat != "ant-ling" || compat.SupportsStrictMode == nil || !*compat.SupportsStrictMode || compat.RequiresReasoningContentOnAssistantMessages != nil {
			t.Errorf("unexpected Ant Ling model compat: %+v", compat)
		}
		level, limit := goai.ThinkingHigh, 123
		conv := &goai.Context{SystemPrompt: "Follow instructions.", Messages: hi.Messages}
		got := captureCompletionsWireV160(t, ring, conv, goai.StreamOptions{Reasoning: &level, MaxTokens: &limit, CacheRetention: goai.CacheRetentionLong, SessionID: "ant-ling-session"})
		assertCompletionsValueV160(t, got.payload, "max_tokens", float64(123))
		assertCompletionsValueV160(t, got.payload, "reasoning", map[string]any{"effort": "high"})
		for _, field := range []string{"max_completion_tokens", "reasoning_effort", "store", "prompt_cache_key", "prompt_cache_retention"} {
			got.field(t, field, "")
		}
		messages, ok := got.payload["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatalf("missing request messages: %#v", got.payload["messages"])
		}
		first, _ := messages[0].(map[string]any)
		if first["role"] != "system" {
			t.Errorf("first message = %#v, want system", first)
		}
	})
	t.Run("direct unmapped medium", func(t *testing.T) {
		adapter := goai.GetApiProvider(ring.Api)
		if adapter == nil || adapter.Stream == nil {
			t.Fatal("missing direct Completions adapter")
		}
		medium := goai.ThinkingMedium
		got := captureCompletionsWireUsingV160(t, ring, hi, goai.StreamOptions{Reasoning: &medium}, adapter.Stream)
		got.field(t, "reasoning", "")
	})
	t.Run("non-reasoning model", func(t *testing.T) {
		ling := promptCacheCatalogueV160(t, "ant-ling", "Ling-2.6-flash")
		got := captureCompletionsEffortV160(t, ling, hi, goai.ThinkingHigh)
		got.field(t, "reasoning", "")
	})
}

// Supplemental: the simple route still clamps a requested medium to high.
func TestCompletionsAntLingSimpleClampV160(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheCatalogueV160(t, "ant-ling", "Ring-2.6-1T")
	got := captureCompletionsEffortV160(t, model, completionsChoiceContextV160("Hi"), goai.ThinkingMedium)
	assertCompletionsValueV160(t, got.payload, "reasoning", map[string]any{"effort": "high"})
}
