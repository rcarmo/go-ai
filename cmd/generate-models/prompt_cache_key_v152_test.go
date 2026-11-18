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
