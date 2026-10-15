package goai

import (
	"encoding/json"
	"testing"
)

// Keep this fixture in the pinned API's wire shape, independent of the Go DTO.
func TestOpenRouter120PinnedPricingWire(t *testing.T) {
	var pricing openRouterPricing
	err := json.Unmarshal([]byte(`{"prompt":"0.000000123456789","completion":"0.000002","input_cache_read":"-1","overrides":[{"min_prompt_tokens":100,"completion":"0.000003"},{"min_prompt_tokens":200,"utc_start":0},{"min_prompt_tokens":300,"utc_end":null},{"min_prompt_tokens":400,"utc_days":[]},{"prompt":"0.1"},{"min_prompt_tokens":0,"prompt":"0.1"}]}`), &pricing)
	if err != nil {
		t.Fatal(err)
	}
	cost := openRouterCatalogCost(&pricing)
	if cost.Input != 0.123457 || cost.Output != 2 || cost.CacheRead != 0 || len(cost.Tiers) != 1 {
		t.Fatal("pinned pricing conversion", cost)
	}
	tier := cost.Tiers[0]
	if tier.InputTokensAbove != 100 || tier.Input != cost.Input || tier.Output != 3 || tier.CacheRead != 0 {
		t.Fatal("tier inheritance", tier)
	}
}
