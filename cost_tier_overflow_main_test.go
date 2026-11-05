package goai

import "testing"

// Pricing tier selection must not wrap a positive aggregate of usage counters.
func TestMainCostTierInputSumOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	model := &Model{Cost: ModelCost{Input: 1, Output: 1, CacheRead: 1, CacheWrite: 1, Tiers: []ModelCostTier{{InputTokensAbove: maxInt / 2, Input: 2, Output: 3, CacheRead: 4, CacheWrite: 5}}}}
	usage := &Usage{Input: maxInt/2 + 1, CacheRead: maxInt/2 + 1, Output: 2}
	cost := CalculateCost(model, usage)
	if cost.Output != 6e-6 {
		t.Fatalf("output cost=%v; want tier cost %v", cost.Output, 6e-6)
	}
	boundary := CalculateCost(model, &Usage{Input: maxInt / 2, Output: 2})
	if boundary.Output != 2e-6 {
		t.Fatalf("strict threshold changed: %v", boundary.Output)
	}
	// Signed terms can cancel an intermediate overflow; clamping each add
	// would choose the wrong tier for this otherwise representable total.
	cancelled := CalculateCost(model, &Usage{Input: maxInt, CacheRead: maxInt, CacheWrite: -maxInt, Output: 2})
	if cancelled.Output != 6e-6 {
		t.Fatalf("cancelled sum picked wrong tier: %v", cancelled.Output)
	}
	negative := CalculateCost(model, &Usage{Input: -maxInt, CacheRead: -maxInt, CacheWrite: maxInt, Output: 2})
	if negative.Output != 2e-6 {
		t.Fatalf("negative sum picked wrong tier: %v", negative.Output)
	}
}
