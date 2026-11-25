package openai

import (
	"encoding/json"
	"os"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Use the live catalog for models still present. For the two models removed
// in pi v1.6.1, replay the full published v1.6.0 data without registering it.
// The fixture records are from dist/providers/data/{groq,opencode}.json in
// pi-ai-1.6.0.tgz (SHA-256 d68312ec574bc959cb7fee26da4f4876d067aed9bbbe76e876359e60d2294747).
func completionsCatalogueWitnessV160(t *testing.T, provider goai.Provider, id string) *goai.Model {
	t.Helper()
	if !(provider == goai.ProviderGroq && id == "qwen/qwen3.6-27b") &&
		!(provider == goai.ProviderOpenCode && id == "kimi-k2.6") {
		return promptCacheCatalogueV160(t, provider, id)
	}
	if model := goai.GetModel(provider, id); model != nil {
		t.Fatalf("retired %s/%s is present in the active v1.6.1 catalog", provider, id)
	}
	data, err := os.ReadFile("testdata/openai-completions-v1.6.0-retired-models.json")
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		goai.Model
		Compat *goai.OpenAICompletionsCompat `json:"compat"`
	}
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Provider == provider && record.ID == id {
			if record.Type != string(goai.ModelTypeChat) || record.Api != goai.ApiOpenAICompletions || record.Compat == nil {
				t.Fatalf("invalid published v1.6.0 Completions record %s/%s", provider, id)
			}
			record.Model.CompletionsCompat = record.Compat
			return &record.Model
		}
	}
	t.Fatalf("missing published v1.6.0 Completions record %s/%s", provider, id)
	return nil
}
