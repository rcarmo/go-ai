package goai

import (
	"encoding/json"
	"strconv"
	"testing"
)

// Usage exposes int counters, unlike the upstream JavaScript number contract.
// A positive sum outside int range must not silently become negative.
func TestMainClassifierUsageSumOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	count := maxInt/2 + 1024
	// The upstream parses JSON into IEEE-754 numbers before using the counts.
	wantCount := int(float64(count))
	number := json.Number(strconv.FormatInt(int64(count), 10))
	got := parseClassifierUsage(map[string]any{
		"input_tokens": number, "output_tokens": number,
	}, &ClassifierModel{Cost: ModelCost{Input: 1, Output: 2}})
	if got == nil {
		t.Fatal("missing usage")
	}
	if got.Input != wantCount || got.Output != wantCount || got.TotalTokens != maxInt {
		t.Fatalf("got input=%d output=%d total=%d; want retained counts and saturated total", got.Input, got.Output, got.TotalTokens)
	}
	if got.Cost.Input != float64(wantCount)/1e6 || got.Cost.Output != 2*float64(wantCount)/1e6 {
		t.Fatalf("cost does not retain individual counts: %+v", got.Cost)
	}

	ordinary := parseClassifierUsage(map[string]any{"input_tokens": json.Number("10"), "output_tokens": json.Number("5")}, &ClassifierModel{})
	if ordinary == nil || ordinary.Input != 10 || ordinary.Output != 5 || ordinary.TotalTokens != 15 {
		t.Fatalf("ordinary usage changed: %+v", ordinary)
	}
	invalid := parseClassifierUsage(map[string]any{"input_tokens": "10", "output_tokens": json.Number("-2")}, &ClassifierModel{})
	if invalid == nil || invalid.Input != 0 || invalid.Output != 0 || invalid.TotalTokens != 0 {
		t.Fatalf("invalid usage changed: %+v", invalid)
	}
}
