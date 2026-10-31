package google

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestGoogle130Gemini4PlusUsesThinkingLevel(t *testing.T) {
	level := goai.ThinkingMedium
	for _, id := range []string{"gemini-4-argon", "gemini-5-flash", "gemini-10-pro", "gemini-20-pro"} {
		model := &goai.Model{ID: id, Reasoning: true}
		req, err := buildRequest(model, &goai.Context{}, &goai.StreamOptions{Reasoning: &level})
		if err != nil {
			t.Fatal(err)
		}
		thinking := req.GenerationConfig.ThinkingConfig
		if thinking == nil || thinking.ThinkingLevel != "MEDIUM" || thinking.ThinkingBudget != nil {
			t.Fatal(id, thinking)
		}
	}
}
