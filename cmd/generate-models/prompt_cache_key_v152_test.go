package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSourceV152PromptCacheKeyModesSurviveCatalogueGeneration(t *testing.T) {
	for _, api := range []string{"openai-completions", "openai-responses"} {
		t.Run(api, func(t *testing.T) {
			var compat compatEntry
			if err := json.Unmarshal([]byte(`{"promptCacheKeyMode":"disabled"}`), &compat); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			writeCompat(&out, api, compat)
			if !strings.Contains(out.String(), `PromptCacheKeyMode: "disabled"`) {
				t.Fatalf("catalogue dropped policy: %q", out.String())
			}
		})
	}
}

func TestCompletionsCatalogueAffinityFormatV160(t *testing.T) {
	var compat compatEntry
	if err := json.Unmarshal([]byte(`{"sessionAffinityFormat":"openai-nosession"}`), &compat); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	writeCompat(&out, "openai-completions", compat)
	if !strings.Contains(out.String(), `SessionAffinityFormat: "openai-nosession"`) {
		t.Fatalf("catalogue dropped session-affinity format: %q", out.String())
	}
}
