package google

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestGoogle130Gemini4PlusUsesThinkingLevel(t *testing.T) {
	level := goai.ThinkingMedium
	for _, id := range []string{"gemini-4-argon", "gemini-5-flash", "gemini-10-pro", "gemini-20-pro"} {
		model := &goai.Model{ID: id, Reasoning: true}
		req := buildRequest(model, &goai.Context{}, &goai.StreamOptions{Reasoning: &level})
		thinking := req.GenerationConfig.ThinkingConfig
		if thinking == nil || thinking.ThinkingLevel != "MEDIUM" || thinking.ThinkingBudget != nil {
			t.Fatal(id, thinking)
		}
	}
}
