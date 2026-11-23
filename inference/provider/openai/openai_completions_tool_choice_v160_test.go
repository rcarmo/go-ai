package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func completionsChoiceContextV160(prompt string, tools ...goai.Tool) *goai.Context {
	return &goai.Context{Messages: []goai.Message{goai.UserMessage(prompt)}, Tools: tools}
}

func completionsChoiceFunctionV160(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected a nonempty tools array, got %#v", payload["tools"])
	}
	first, ok := tools[0].(map[string]any)
	if ok {
		if fn, ok := first["function"].(map[string]any); ok && fn != nil {
			return fn
		}
	}
	t.Fatalf("expected a function object in first tool, got %#v", tools[0])
	return nil
}

func completionsChoiceRequiredV160(t *testing.T, fn map[string]any, want []any) {
	t.Helper()
	params, ok := fn["parameters"].(map[string]any)
	if !ok || !reflect.DeepEqual(params["required"], want) {
		t.Fatalf("expected parameters.required == %v, got %#v", want, fn["parameters"])
	}
}

// Source: pi v1.6.0 packages/ai/test/openai-completions-tool-choice.test.ts:133-317.
// The original samples the SDK params; the native test compares their wire
// equivalents at the actual HTTP request boundary via a non-dialing transport.
func TestSourceV160OpenAICompletionsToolChoice(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	openai := promptCacheModelV160(t, "", "", "")
	ping := goai.Tool{Name: "ping", Description: "Ping tool", Parameters: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`)}
	preferStrict := goai.Tool{
		Name: "ping", Description: "Ping tool",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"required":{"type":"string"},"optional":{"type":"string"}},"required":["required"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"},
	}
	// A non-nil off effort selects the native simple adapter through Stream.
	off := goai.ThinkingLevel(goai.ThinkingOff)
	capture := func(t *testing.T, model *goai.Model, ctx *goai.Context, choice goai.ToolChoice) promptCacheRequestV160 {
		t.Helper()
		return captureCompletionsWireV160(t, model, ctx, goai.StreamOptions{ToolChoice: choice, Reasoning: &off})
	}
	t.Run("required from simple options", func(t *testing.T) {
		got := capture(t, openai, completionsChoiceContextV160("Call ping with ok=true", ping), goai.ToolChoiceRequired)
		got.field(t, "tool_choice", "required")
		tools, isArray := got.payload["tools"].([]any)
		if !isArray || len(tools) == 0 {
			t.Fatalf("want a nonempty tools array, got %#v", got.payload["tools"])
		}
	})
	t.Run("none even without tools", func(t *testing.T) {
		got := capture(t, openai, completionsChoiceContextV160("Summarize the conversation"), goai.ToolChoiceNone)
		got.field(t, "tool_choice", "none")
		got.field(t, "tools", "")
	})
	t.Run("explicit strict opt-out", func(t *testing.T) {
		model := promptCacheModelV160(t, "", "", `{"supportsStrictMode":false}`)
		got := capture(t, model, completionsChoiceContextV160("Call ping with ok=true", ping), "")
		fn := completionsChoiceFunctionV160(t, got.payload)
		if value, present := fn["strict"]; present {
			t.Fatalf("strict = %#v, want omitted", value)
		}
	})
	t.Run("unknown compatible endpoint", func(t *testing.T) {
		model := &goai.Model{
			ID: "local-model", Name: "Local Model", Api: goai.ApiOpenAICompletions, Provider: "local-vllm",
			BaseURL: "http://localhost:8000/v1", Input: []string{"text"}, Reasoning: true,
			ContextWindow: 128_000, MaxTokens: 4096,
		}
		got := capture(t, model, completionsChoiceContextV160("Call ping", preferStrict), "")
		fn := completionsChoiceFunctionV160(t, got.payload)
		if value, present := fn["strict"]; present {
			t.Fatalf("strict = %#v, want omitted", value)
		}
		completionsChoiceRequiredV160(t, fn, []any{"required"})
	})
	t.Run("capable Groq catalogue model", func(t *testing.T) {
		model := promptCacheCatalogueV160(t, "groq", "openai/gpt-oss-20b")
		if compat := model.CompletionsCompat; compat == nil || compat.SupportsStrictMode == nil || !*compat.SupportsStrictMode {
			t.Fatalf("catalogue model must declare supportsStrictMode=true, got %+v", compat)
		}
		got := capture(t, model, completionsChoiceContextV160("Call ping", preferStrict), "")
		fn := completionsChoiceFunctionV160(t, got.payload)
		if fn["strict"] != true {
			t.Fatalf("strict = %#v, want true", fn["strict"])
		}
		completionsChoiceRequiredV160(t, fn, []any{"required", "optional"})
	})
}

// Supplemental public options, not assertions in the pinned test. The named
// forced choice uses SamplingParams, which preserves provider-specific objects.
func TestOpenAICompletionsToolChoiceWireSupplementV160(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheModelV160(t, "", "", "")
	ping := goai.Tool{Name: "ping", Description: "Ping tool", Parameters: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`)}
	ctx := completionsChoiceContextV160("Call ping", ping)
	forced := map[string]any{"type": "function", "function": map[string]any{"name": "ping"}}
	for _, tc := range []struct {
		name, optionsJSON string
		forced            bool
		want              any
	}{
		{name: "unset", optionsJSON: `{}`},
		{name: "explicit null option", optionsJSON: `{"toolChoice":null}`},
		{name: "auto", optionsJSON: `{"toolChoice":"auto"}`, want: "auto"},
		{name: "named forced function", optionsJSON: `{}`, forced: true, want: forced},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts goai.StreamOptions
			if err := json.Unmarshal([]byte(tc.optionsJSON), &opts); err != nil {
				t.Fatal(err)
			}
			off := goai.ThinkingLevel(goai.ThinkingOff)
			opts.Reasoning = &off
			if tc.forced {
				opts.SamplingParams = map[string]any{"tool_choice": forced}
			}
			payload := captureCompletionsWireV160(t, model, ctx, opts).payload
			got, present := payload["tool_choice"]
			if present != (tc.want != nil) || (present && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("tool_choice = %#v (present=%t), want %#v", got, present, tc.want)
			}
		})
	}
}
