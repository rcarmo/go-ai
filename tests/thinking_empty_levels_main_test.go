package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source models.ts:getSupportedThinkingLevels filters all explicitly disabled
// levels. clampThinkingLevel still returns off when no candidate exists.
func TestMainThinkingAllLevelsDisabled(t *testing.T) {
	m := &goai.Model{Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{
		"off": nil, "minimal": nil, "low": nil, "medium": nil, "high": nil, "xhigh": nil, "max": nil,
	}}
	if got := goai.GetSupportedThinkingLevels(m); len(got) != 0 {
		t.Fatalf("supported=%v; want empty", got)
	}
	if got := goai.ClampThinkingLevel(m, "high"); got != "off" {
		t.Fatalf("clamp=%q; want off fallback", got)
	}
	if mapped, ok := goai.MapThinkingLevel(m, "high"); ok {
		t.Fatalf("disabled off mapping reported as available: %q", mapped)
	}
	if got := goai.GetSupportedThinkingLevels(&goai.Model{}); len(got) != 1 || got[0] != "off" {
		t.Fatalf("non-reasoning control=%v", got)
	}
}
