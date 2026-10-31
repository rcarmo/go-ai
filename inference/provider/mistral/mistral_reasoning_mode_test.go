package mistral

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func makeMistralContext() *goai.Context {
	return &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}
}

func TestMistralReasoningModeUsesReasoningEffortForMistralSmall4(t *testing.T) {
	reasoning := goai.ThinkingMedium
	model := &goai.Model{ID: "mistral-small-2603", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{Reasoning: &reasoning})
	if payload.ReasoningEffort != "high" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want high/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeOmitsReasoningControlsForMistralSmall4WhenThinkingOff(t *testing.T) {
	model := &goai.Model{ID: "mistral-small-2603", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), nil)
	if payload.ReasoningEffort != "" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want empty/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeUsesPromptModeForMagistralReasoningModels(t *testing.T) {
	reasoning := goai.ThinkingMedium
	model := &goai.Model{ID: "magistral-medium-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{Reasoning: &reasoning})
	if payload.PromptMode != "reasoning" || payload.ReasoningEffort != "" {
		t.Fatalf("promptMode=%q reasoningEffort=%q, want reasoning/empty", payload.PromptMode, payload.ReasoningEffort)
	}
}

func TestMistralReasoningModeUsesReasoningEffortForMistralMedium35(t *testing.T) {
	reasoning := goai.ThinkingMedium
	model := &goai.Model{ID: "mistral-medium-3.5", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{Reasoning: &reasoning})
	if payload.ReasoningEffort != "high" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want high/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeOmitsReasoningControlsForMistralMedium35WhenThinkingOff(t *testing.T) {
	model := &goai.Model{ID: "mistral-medium-3.5", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), nil)
	if payload.ReasoningEffort != "" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want empty/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeUsesSessionIDAsPromptCacheKey(t *testing.T) {
	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{SessionID: "session-123"})
	if payload.PromptCacheKey != "session-123" {
		t.Fatalf("promptCacheKey=%q, want session-123", payload.PromptCacheKey)
	}
}

func TestMistralReasoningModeOmitsPromptCacheKeyWhenCacheRetentionDisabled(t *testing.T) {
	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{SessionID: "session-123", CacheRetention: goai.CacheRetentionNone})
	if payload.PromptCacheKey != "" {
		t.Fatalf("promptCacheKey=%q, want empty", payload.PromptCacheKey)
	}
}

func strPtr(s string) *string { return &s }

// Cases follow pinned streamSimple's map-presence, off and nullish-high rules.
func TestMistralReasoningCapabilitiesSourceMain(t *testing.T) {
	medium, off := goai.ThinkingMedium, goai.ThinkingLevel(goai.ThinkingOff)
	for _, tc := range []struct {
		name, id       string
		reasoning      bool
		mapping        map[goai.ModelThinkingLevel]*string
		level          *goai.ThinkingLevel
		effort, prompt string
	}{
		{"custom-map", "custom", true, map[goai.ModelThinkingLevel]*string{"medium": strPtr("low")}, &medium, "low", ""},
		{"named-without-map", "mistral-small-2603", true, nil, &medium, "", "reasoning"},
		{"default-off", "custom", true, map[goai.ModelThinkingLevel]*string{"off": strPtr("none")}, nil, "none", ""},
		{"explicit-off", "custom", true, map[goai.ModelThinkingLevel]*string{"off": strPtr("none")}, &off, "none", ""},
		{"null-off", "custom", true, map[goai.ModelThinkingLevel]*string{"off": nil}, nil, "", ""},
		{"missing-off", "custom", true, map[goai.ModelThinkingLevel]*string{}, nil, "", ""},
		{"missing-level-fallback", "custom", true, map[goai.ModelThinkingLevel]*string{}, &medium, "high", ""},
		{"non-reasoning", "custom", false, map[goai.ModelThinkingLevel]*string{"off": strPtr("none")}, &medium, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: tc.id, Provider: goai.ProviderMistral, Reasoning: tc.reasoning, ThinkingLevelMap: tc.mapping}
			payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{Reasoning: tc.level})
			if payload.ReasoningEffort != tc.effort || payload.PromptMode != tc.prompt {
				t.Fatalf("got %q/%q want %q/%q", payload.ReasoningEffort, payload.PromptMode, tc.effort, tc.prompt)
			}
		})
	}
}

func TestMistralToolChoiceSourceMain(t *testing.T) {
	for _, choice := range []goai.ToolChoice{"", goai.ToolChoiceAuto, goai.ToolChoiceNone, goai.ToolChoiceRequired} {
		t.Run(string(choice), func(t *testing.T) {
			payload := buildRequest(&goai.Model{ID: "test"}, makeMistralContext(), &goai.StreamOptions{ToolChoice: choice})
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err = json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			value, present := wire["tool_choice"]
			if choice == "" {
				if present {
					t.Fatal("unset choice was emitted", value)
				}
			} else if value != string(choice) {
				t.Fatal("wrong wire tool choice", value, choice)
			}
		})
	}
}
