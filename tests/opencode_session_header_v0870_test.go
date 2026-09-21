package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0870OpenCodeSessionHeaderHelperAddsAndPreserves(t *testing.T) {
	added := goai.WithOpenCodeSessionHeader(goai.ProviderOpenCode, "session-1", map[string]string{"X-Test": "ok"})
	if added["x-opencode-session"] != "session-1" || added["X-Test"] != "ok" {
		t.Fatalf("added headers=%#v", added)
	}
	preserved := goai.WithOpenCodeSessionHeader(goai.ProviderOpenCodeGo, "session-2", map[string]string{"X-OpenCode-Session": "caller"})
	if preserved["X-OpenCode-Session"] != "caller" || preserved["x-opencode-session"] != "" {
		t.Fatalf("preserved headers=%#v", preserved)
	}
	other := goai.WithOpenCodeSessionHeader(goai.ProviderOpenAI, "session-3", map[string]string{})
	if _, ok := other["x-opencode-session"]; ok {
		t.Fatalf("non-opencode provider got session header: %#v", other)
	}
}
