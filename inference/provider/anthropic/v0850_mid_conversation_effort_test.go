package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0850AnthropicMidConversationEffortMarkersAndBinding(t *testing.T) {
	reasoningLow := goai.ThinkingLow
	reasoningHigh := goai.ThinkingHigh
	model := v0850ManagedEffortModel()

	first := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("one")}}, &goai.StreamOptions{Reasoning: &reasoningLow})
	if len(first.Messages) != 2 {
		t.Fatalf("first messages len=%d, want user+marker: %#v", len(first.Messages), first.Messages)
	}
	assertV0850EffortMarker(t, first.Messages[1], "low")
	if first.OutputConfig == nil || first.OutputConfig.Effort != "high" {
		t.Fatalf("top-level output_config=%#v, want high", first.OutputConfig)
	}
	if first.Thinking == nil || first.Thinking.Type != "adaptive" || first.Thinking.Display != "summarized" || first.Thinking.BlockBinding == nil || first.Thinking.BlockBinding.PrefixMismatchBehavior != "drop_block" {
		t.Fatalf("thinking=%#v, want adaptive summarized drop_block", first.Thinking)
	}
	if first.Temperature != nil {
		t.Fatalf("temperature=%#v, want omitted for managed effort", first.Temperature)
	}

	history := goai.Message{
		Role:                  goai.RoleAssistant,
		Api:                   goai.ApiAnthropicMessages,
		Provider:              model.Provider,
		Model:                 model.ID,
		ProviderThinkingLevel: "low",
		StopReason:            goai.StopReasonStop,
		Usage:                 &goai.Usage{},
		Content: []goai.ContentBlock{
			{Type: "thinking", Thinking: "reasoning", ThinkingSignature: "sig"},
			{Type: "text", Text: "answer"},
		},
	}
	otherProvider := history
	otherProvider.Provider = "other-provider"
	legacy := history
	legacy.ProviderThinkingLevel = ""

	second := buildRequest(model, &goai.Context{Messages: []goai.Message{
		goai.UserMessage("one"),
		history,
		goai.UserMessage("two"),
		otherProvider,
		goai.UserMessage("three"),
		legacy,
		goai.UserMessage("four"),
	}}, &goai.StreamOptions{Reasoning: &reasoningHigh, Temperature: floatPtrV0850(0.2)})

	var efforts []string
	for _, message := range second.Messages {
		if message.Role == "system" {
			if message.OutputConfig == nil {
				t.Fatalf("system marker missing output_config: %#v", message)
			}
			efforts = append(efforts, message.OutputConfig.Effort)
		}
	}
	want := []string{"low", "high"}
	if len(efforts) != len(want) || efforts[0] != want[0] || efforts[1] != want[1] {
		t.Fatalf("effort markers=%v, want %v; messages=%#v", efforts, want, second.Messages)
	}
}

func TestV0850AnthropicManagedEffortHeadersAndResultThinkingLevel(t *testing.T) {
	var beta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beta = r.Header.Get("Anthropic-Beta")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte("data: {\"message\":{\"id\":\"msg_1\",\"model\":\"claude-fable-5-1\",\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer server.Close()

	model := v0850ManagedEffortModel()
	model.BaseURL = server.URL
	reasoning := goai.ThinkingLow
	var done *goai.Message
	for ev := range streamAnthropic(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("one")}}, &goai.StreamOptions{APIKey: "key", Reasoning: &reasoning}) {
		if e, ok := ev.(*goai.DoneEvent); ok {
			done = e.Message
		}
	}
	if !strings.Contains(beta, midConversationOutputConfigBeta) || !strings.Contains(beta, thinkingBindingControlsBeta) {
		t.Fatalf("Anthropic-Beta=%q missing managed effort betas", beta)
	}
	if done == nil || done.StopReason != goai.StopReasonStop || done.ProviderThinkingLevel != "low" {
		data, _ := json.Marshal(done)
		t.Fatalf("done=%s, want stop with providerThinkingLevel low", data)
	}
}

func TestV0850AnthropicSupportsMidConvoEffortCatalogGates(t *testing.T) {
	goai.RegisterBuiltinModels()
	direct := goai.GetModel(goai.ProviderAnthropic, "claude-fable-5-1")
	if direct == nil || direct.AnthropicCompat == nil || direct.AnthropicCompat.SupportsMidConvoEffort == nil || !*direct.AnthropicCompat.SupportsMidConvoEffort {
		t.Fatalf("direct claude-fable-5-1 supportsMidConvoEffort not generated: %#v", direct)
	}
	if direct.ThinkingLevelMap[goai.ThinkingOff] != nil {
		t.Fatalf("direct off thinking map=%#v, want nil unsupported off", direct.ThinkingLevelMap[goai.ThinkingOff])
	}
	openRouter := goai.GetModel(goai.ProviderOpenRouter, "anthropic/claude-fable-5.1")
	if openRouter == nil || openRouter.Api != goai.ApiAnthropicMessages || openRouter.BaseURL != "https://openrouter.ai/api" || openRouter.AnthropicCompat == nil || openRouter.AnthropicCompat.SupportsMidConvoEffort == nil || !*openRouter.AnthropicCompat.SupportsMidConvoEffort {
		t.Fatalf("openrouter fable 5.1 managed effort metadata mismatch: %#v", openRouter)
	}
	unsupported := goai.GetModel(goai.ProviderAnthropic, "claude-opus-4-8")
	if unsupported == nil || unsupported.AnthropicCompat == nil || unsupported.AnthropicCompat.SupportsMidConvoEffort != nil {
		t.Fatalf("opus-4-8 supportsMidConvoEffort=%#v, want unset", unsupported.AnthropicCompat)
	}
	fallbackSource := goai.GetModel(goai.ProviderAnthropic, "claude-opus-5")
	if fallbackSource == nil || fallbackSource.AnthropicCompat == nil || len(fallbackSource.AnthropicCompat.AllowedFallbackModels) != 0 {
		t.Fatalf("opus-5 fallback metadata=%#v, want none", fallbackSource)
	}
}

func v0850ManagedEffortModel() *goai.Model {
	return &goai.Model{
		ID:               "claude-fable-5-1",
		Name:             "Claude Fable 5.1",
		Api:              goai.ApiAnthropicMessages,
		Provider:         goai.ProviderAnthropic,
		BaseURL:          "http://127.0.0.1:9",
		Reasoning:        true,
		ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil, goai.ModelThinkingLevel(goai.ThinkingLow): stringPtrV0850("low"), goai.ModelThinkingLevel(goai.ThinkingHigh): stringPtrV0850("high")},
		Input:            []string{"text"},
		Cost:             goai.ModelCost{},
		ContextWindow:    200000,
		MaxTokens:        32000,
		AnthropicCompat:  &goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(true), SupportsMidConvoEffort: boolPtrCompat(true)},
	}
}

func assertV0850EffortMarker(t *testing.T, got anthropicMessage, effort string) {
	t.Helper()
	if got.Role != "system" || got.OutputConfig == nil || got.OutputConfig.Effort != effort {
		t.Fatalf("marker=%#v, want system effort %q", got, effort)
	}
	blocks, ok := got.Content.([]anthropicContentBlock)
	if !ok || len(blocks) != 0 {
		t.Fatalf("marker content=%#v (%T), want empty block slice", got.Content, got.Content)
	}
}

func stringPtrV0850(s string) *string  { return &s }
func floatPtrV0850(v float64) *float64 { return &v }
