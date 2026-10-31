package google

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0843GoogleThinkingLevelMapResolvesMappedLevels(t *testing.T) {
	low := "low"
	model := &goai.Model{ID: "gemini-3-flash", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingXHigh): &low}}
	level, err := resolveGoogleThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingXHigh))
	if err != nil || level != "low" {
		t.Fatalf("level=%q err=%v, want low nil", level, err)
	}
	if got := getGoogleThinkingLevel(level, model); got != "LOW" {
		t.Fatalf("api thinking level=%q, want LOW", got)
	}
}

func TestV0843GoogleThinkingLevelMapRejectsUnsupportedMappings(t *testing.T) {
	bad := "max"
	model := &goai.Model{ID: "gemini-3-flash", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingHigh): &bad, goai.ModelThinkingLevel(goai.ThinkingLow): nil}}
	if _, err := resolveGoogleThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingHigh)); err == nil {
		t.Fatal("expected unsupported mapping error for high -> max")
	}
	// The pinned resolver accepts a standard logical level when its mapping is
	// null. The production Simple path must clamp unsupported levels first.
	if level, err := resolveGoogleThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingLow)); err != nil || level != "low" {
		t.Fatalf("null standard mapping: %q / %v", level, err)
	}
	low := goai.ThinkingLow
	req, err := buildRequest(model, &goai.Context{}, &goai.StreamOptions{Reasoning: &low})
	if err != nil {
		t.Fatal(err)
	}
	if config := requireThinkingConfig(t, req); config.ThinkingLevel != "MEDIUM" {
		t.Fatalf("unsupported low must clamp to medium: %#v", config)
	}
	model.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingXHigh)] = nil
	if _, err := resolveGoogleThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingXHigh)); err == nil {
		t.Fatal("expected unsupported null extended mapping error for xhigh")
	}
}

func TestV0843GoogleBudgetUsesMappedLevel(t *testing.T) {
	medium := "medium"
	model := &goai.Model{ID: "gemini-2.5-flash", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingXHigh): &medium}}
	level, err := resolveGoogleThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingXHigh))
	if err != nil {
		t.Fatal(err)
	}
	if got := getGoogleBudget(model, level); got != 8192 {
		t.Fatalf("budget=%d, want mapped medium budget 8192", got)
	}
}
