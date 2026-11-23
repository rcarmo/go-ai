package openai

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func openCodeGoModelWithoutCompatV160(t *testing.T) goai.Model {
	t.Helper()
	model := *promptCacheCatalogueV160(t, goai.ProviderOpenCodeGo, "kimi-k3")
	model.Api, model.CompletionsCompat, model.ResponsesCompat = goai.ApiOpenAICompletions, nil, nil
	return model
}

func replayReadCallV160() goai.ContentBlock {
	return goai.ContentBlock{Type: "toolCall", ID: "call_1", Name: "read", Arguments: map[string]any{"path": "README.md"}}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1243-1299.
func TestSourceV160CompletionsXiaomiReplay(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := promptCacheCatalogueV160(t, goai.ProviderXiaomi, "mimo-v2.6-pro")
	conv := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("Read README.md"),
		{Role: goai.RoleAssistant, Api: goai.ApiOpenAICompletions, Provider: goai.ProviderXiaomi, Model: "mimo-v2.6-pro",
			StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{replayReadCallV160()}},
		{Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "read", Content: []goai.ContentBlock{{Type: "text", Text: "contents"}}},
	}}
	got := captureCompletionsEffortV160(t, model, conv, goai.ThinkingHigh)
	if !reflect.DeepEqual(got.payload["thinking"], map[string]any{"type": "enabled"}) {
		t.Errorf("thinking=%#v, want exactly type enabled", got.payload["thinking"])
	}
	got.field(t, "reasoning_effort", "")
	messages, _ := got.payload["messages"].([]any)
	for _, item := range messages {
		if msg, ok := item.(map[string]any); ok && msg["role"] == "assistant" {
			if value, present := msg["reasoning_content"]; !present || value != "" {
				t.Fatalf("assistant reasoning_content=%#v (present=%t), want present empty string", value, present)
			}
			return
		}
	}
	t.Fatal("missing assistant replay message")
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1301-1353.
func TestSourceV160CompletionsProviderReasoningSignature(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	openCode := openCodeGoModelWithoutCompatV160(t)
	for _, tc := range []struct {
		model    *goai.Model
		line     int
		wantJSON string
	}{
		{&openCode, 1301, `[{"type":"thinking","thinking":"think","thinkingSignature":"reasoning_content"}]`},
		{promptCacheModelV160(t, "", "", ""), 1328, `[{"type":"thinking","thinking":"think","thinkingSignature":"reasoning"}]`},
	} {
		t.Run(string(tc.model.Provider), func(t *testing.T) {
			got := observeCompletionsStreamV160(t, tc.model, completionsChoiceContextV160("Use reasoning."), tc.line)
			assertCompletionsContentV160(t, got.message, tc.wantJSON)
		})
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1355-1414.
func TestSourceV160CompletionsOpenCodeGoReplay(t *testing.T) {
	goai.RegisterBuiltinModels()
	t.Setenv("PI_CACHE_RETENTION", "")
	model := openCodeGoModelWithoutCompatV160(t)
	compat := `{
		"supportsStore":false,"supportsDeveloperRole":false,"supportsReasoningEffort":true,
		"supportsUsageInStreaming":true,"supportsFinishReason":true,"maxTokensField":"max_completion_tokens",
		"requiresToolResultName":false,"requiresAssistantAfterToolResult":false,"requiresNonNullAssistantContent":false,
		"requiresToolsWithToolHistory":false,"requiresThinkingAsText":false,"requiresReasoningContentOnAssistantMessages":false,
		"thinkingFormat":"openai","openRouterRouting":{},"vercelGatewayRouting":{},"chatTemplateKwargs":{},"chatTemplateArgs":{},
		"zaiToolStream":false,"supportsStrictMode":true,"supportsOpenAIGrammarTools":false,
		"sendSessionAffinityHeaders":false,"sessionAffinityFormat":"openai","supportsLongCacheRetention":true
	}`
	if err := json.Unmarshal([]byte(compat), &model.CompletionsCompat); err != nil {
		t.Fatal(err)
	}
	conv := &goai.Context{Messages: []goai.Message{{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider,
		Model: model.ID, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{
			{Type: "thinking", Thinking: "think", ThinkingSignature: "reasoning"}, replayReadCallV160(),
		}}}}
	got := captureCompletionsEffortV160(t, &model, conv, "")
	messages, ok := got.payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("missing replay messages: %#v", got.payload["messages"])
	}
	msg, _ := messages[0].(map[string]any)
	if msg["role"] != "assistant" || msg["reasoning_content"] != "think" {
		t.Errorf("first message = %#v, want assistant with reasoning_content=think", msg)
	}
	if value, present := msg["reasoning"]; present {
		t.Errorf("reasoning=%#v (present=true), want omitted", value)
	}
}
