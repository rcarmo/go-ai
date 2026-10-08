package oauth

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestRadius110AccountCatalogReplacesBaselineIncludingEmpty(t *testing.T) {
	p := NewRadiusProvider(RadiusProviderOptions{})
	baseline := []*goai.Model{{ID: "disabled", Provider: goai.Provider(p.id)}, {ID: "other", Provider: "other"}}
	// Config shape uses the same credential decoder as login/refresh.
	for _, models := range []any{[]any{}, []any{map[string]any{"id": "enabled", "name": "enabled", "input": []any{"text"}, "api": "openai-responses", "contextWindow": 1000, "maxTokens": 100}}} {
		creds := &Credentials{Extra: map[string]any{"gatewayConfig": map[string]any{"baseUrl": "https://gateway.example/v1", "models": models}}}
		got := p.ModifyModels(baseline, creds)
		if len(got) != 1+len(models.([]any)) || got[0].ID != "other" {
			t.Fatal("baseline retained", got)
		}
		for _, m := range got {
			if m.ID == "disabled" {
				t.Fatal("disabled baseline retained")
			}
		}
	}
}
