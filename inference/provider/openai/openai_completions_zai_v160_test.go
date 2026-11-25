package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// The non-nil level selects the simple adapter through public Stream. An empty
// level leaves reasoning effectively unset, as in the source options.
func captureCompletionsEffortV160(t *testing.T, model *goai.Model, ctx *goai.Context, level goai.ThinkingLevel) promptCacheRequestV160 {
	t.Helper()
	return captureCompletionsWireV160(t, model, ctx, goai.StreamOptions{Reasoning: &level})
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:319-373.
func TestSourceV160CompletionsGroqEffort(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	for _, tc := range []struct{ id, want string }{
		{"qwen/qwen3.6-27b", "default"},
		{"openai/gpt-oss-20b", "medium"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			model := completionsCatalogueWitnessV160(t, goai.ProviderGroq, tc.id)
			got := captureCompletionsEffortV160(t, model, completionsChoiceContextV160("Hi"), goai.ThinkingMedium)
			got.field(t, "reasoning_effort", tc.want)
		})
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:412-447. Preserve
// the duplicate glm-4.7 lookup and the full maps, including explicit nulls.
func TestSourceV160CompletionsZAICatalogue(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct{ name, id string }{
		{"glm-4.7/first", "glm-4.7"}, {"glm-4.7/repeated", "glm-4.7"},
		{"glm-5-turbo", "glm-5-turbo"}, {"glm-5.2", "glm-5.2"},
	} {
		t.Run("tool-stream/"+tc.name, func(t *testing.T) {
			model := promptCacheCatalogueV160(t, goai.ProviderZAI, tc.id)
			if compat := model.CompletionsCompat; compat == nil || compat.ZaiToolStream == nil || !*compat.ZaiToolStream {
				t.Fatalf("zaiToolStream must be explicitly true, got %+v", compat)
			}
		})
	}
	const glm52 = `{"off":"none","minimal":null,"low":null,"medium":null,"high":"high","xhigh":null,"max":"max"}`
	const glm53 = `{"off":null,"minimal":null,"low":"low","medium":null,"high":"high","xhigh":null,"max":"max"}`
	for _, tc := range []struct {
		provider     goai.Provider
		id, wantJSON string
	}{
		{goai.ProviderZAI, "glm-5.2", glm52},
		{goai.ProviderZAI, "glm-5.2-highspeed", glm52},
		{goai.ProviderZAI, "glm-5.3", glm53},
		{goai.ProviderZAICodingCN, "glm-5.3", glm53},
	} {
		t.Run("effort-map/"+string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			model := promptCacheCatalogueV160(t, tc.provider, tc.id)
			if compat := model.CompletionsCompat; compat == nil || compat.SupportsReasoningEffort == nil || !*compat.SupportsReasoningEffort {
				t.Errorf("supportsReasoningEffort must be explicitly true, got %+v", compat)
			}
			var want map[goai.ModelThinkingLevel]*string
			if err := json.Unmarshal([]byte(tc.wantJSON), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(model.ThinkingLevelMap, want) {
				actual, _ := json.Marshal(model.ThinkingLevelMap)
				t.Errorf("thinkingLevelMap = %s, want %s", actual, tc.wantJSON)
			}
		})
	}
}

func assertCompletionsZAIThinkingV160(t *testing.T, got map[string]any, enabled bool) {
	t.Helper()
	want := map[string]any{"type": "disabled"}
	if enabled {
		want = map[string]any{"type": "enabled", "clear_thinking": false}
	}
	if !reflect.DeepEqual(got["thinking"], want) {
		t.Errorf("thinking = %#v, want %#v", got["thinking"], want)
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:375-410,449-644.
func TestSourceV160CompletionsZAIWire(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheCatalogueV160(t, goai.ProviderZAI, "glm-5.2")
	hi := completionsChoiceContextV160("Hi")
	ping := goai.Tool{Name: "ping", Description: "Ping tool", Parameters: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`)}
	withTools := completionsChoiceContextV160("Call ping with ok=true", ping)
	t.Run("tool-stream with tools", func(t *testing.T) {
		got := captureCompletionsEffortV160(t, model, withTools, "")
		if got.payload["tool_stream"] != true {
			t.Fatalf("tool_stream = %#v, want true", got.payload["tool_stream"])
		}
	})
	for _, tc := range []struct {
		level goai.ThinkingLevel
		want  string
	}{
		{goai.ThinkingLow, "high"}, {goai.ThinkingMedium, "high"},
		{goai.ThinkingHigh, "high"}, {goai.ThinkingMax, "max"},
	} {
		t.Run("effort/"+string(tc.level), func(t *testing.T) {
			got := captureCompletionsEffortV160(t, model, hi, tc.level)
			assertCompletionsZAIThinkingV160(t, got.payload, true)
			got.field(t, "reasoning_effort", tc.want)
		})
	}
	t.Run("replayed reasoning", func(t *testing.T) {
		ctx := &goai.Context{Messages: []goai.Message{
			goai.UserMessage("Read README.md"),
			{Role: goai.RoleAssistant, Api: goai.ApiOpenAICompletions, Provider: goai.ProviderZAI, Model: "glm-5.2", StopReason: goai.StopReasonToolUse,
				Content: []goai.ContentBlock{
					{Type: "thinking", Thinking: "prior reasoning", ThinkingSignature: "reasoning_content"},
					{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "README.md"}},
				}},
			{Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "read", Content: []goai.ContentBlock{{Type: "text", Text: "contents"}}},
			goai.UserMessage("Continue"),
		}}
		got := captureCompletionsEffortV160(t, model, ctx, goai.ThinkingHigh)
		assertCompletionsZAIThinkingV160(t, got.payload, true)
		messages, _ := got.payload["messages"].([]any)
		for _, item := range messages {
			msg, ok := item.(map[string]any)
			if ok && msg["role"] == "assistant" {
				if msg["reasoning_content"] != "prior reasoning" {
					t.Fatalf("assistant reasoning_content = %#v, want prior reasoning", msg["reasoning_content"])
				}
				return
			}
		}
		t.Fatal("missing assistant replay message")
	})
	t.Run("off omits effort", func(t *testing.T) {
		got := captureCompletionsEffortV160(t, model, hi, "")
		assertCompletionsZAIThinkingV160(t, got.payload, false)
		got.field(t, "reasoning_effort", "")
	})
	t.Run("explicit tool-stream override", func(t *testing.T) {
		variant := *model
		compat := *model.CompletionsCompat
		value := true
		compat.ZaiToolStream = &value
		variant.CompletionsCompat = &compat
		got := captureCompletionsEffortV160(t, &variant, withTools, "")
		if got.payload["tool_stream"] != true {
			t.Fatalf("tool_stream = %#v, want true", got.payload["tool_stream"])
		}
	})
	t.Run("no tools", func(t *testing.T) {
		got := captureCompletionsEffortV160(t, model, hi, "")
		got.field(t, "tool_stream", "")
	})
}

// Supplemental: the capability controls reasoning_effort, not thinking mode.
func TestCompletionsZAIEffortOptOutV160(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := *promptCacheCatalogueV160(t, goai.ProviderZAI, "glm-5.2")
	compat := *model.CompletionsCompat
	supportsEffort := false
	compat.SupportsReasoningEffort = &supportsEffort
	model.CompletionsCompat = &compat
	got := captureCompletionsEffortV160(t, &model, completionsChoiceContextV160("Hi"), goai.ThinkingHigh)
	assertCompletionsZAIThinkingV160(t, got.payload, true)
	got.field(t, "reasoning_effort", "")
}
