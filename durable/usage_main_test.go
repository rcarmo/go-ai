package durable

import (
	"math"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUsageMainFirstRecordProvenanceAndBilledTotals(t *testing.T) {
	for _, bucket := range []string{"models", "tools"} {
		for _, mode := range []string{"missing", "estimated", "billed"} {
			t.Run(bucket+"/"+mode, func(t *testing.T) {
				state := JSON{"models": map[string]any{}, "tools": map[string]any{}}
				var flag *bool
				if mode != "missing" {
					billed := mode == "billed"
					flag = &billed
				}
				first := goai.Usage{Input: 1, TotalTokens: 1, Cost: goai.CostBreakdown{Input: .25, Output: .75, Total: 1, TotalFromProvider: flag}}
				add := func(usage *goai.Usage) error {
					if bucket == "tools" {
						return addToolUsage(state, "__proto__", usage, DefaultLimits())
					}
					return addModelUsage(state, messageReceipt{Provider: "p", Model: "m", Usage: usage}, DefaultLimits())
				}
				if err := add(&first); err != nil {
					t.Fatal(err)
				}
				later := true
				if flag != nil {
					later = !*flag
				}
				next := goai.Usage{Input: 2, TotalTokens: 2, Cost: goai.CostBreakdown{Input: .25, Output: .75, Total: 2, TotalFromProvider: &later}}
				if err := add(&next); err != nil {
					t.Fatal(err)
				}
				if flag != nil {
					*flag = !*flag
				}
				key := "p/m"
				if bucket == "tools" {
					key = "__proto__"
				}
				values := state[bucket].(map[string]any)
				var usage goai.Usage
				if err := fromObject(values[key].(JSON), &usage, DefaultLimits()); err != nil {
					t.Fatal(err)
				}
				if usage.Input != 3 || usage.TotalTokens != 3 || math.Abs(usage.Cost.Total-3) > 1e-12 || usage.Cost.Input != .5 || usage.Cost.Output != 1.5 {
					t.Fatalf("totals=%+v", usage)
				}
				if mode == "missing" {
					if usage.Cost.TotalFromProvider != nil {
						t.Fatal("later provenance invented field")
					}
				} else if usage.Cost.TotalFromProvider == nil || *usage.Cost.TotalFromProvider != (mode == "billed") {
					t.Fatal("first provenance not preserved")
				}
			})
		}
	}
}

func TestUsagePresenceMainLedgerOptionalZero(t *testing.T) {
	for _, bucket := range []string{"models", "tools"} {
		t.Run(bucket, func(t *testing.T) {
			state := JSON{"models": map[string]any{}, "tools": map[string]any{}}
			add := func(u *goai.Usage) error {
				if bucket == "tools" {
					return addToolUsage(state, "tool", u, DefaultLimits())
				}
				return addModelUsage(state, messageReceipt{Provider: "p", Model: "m", Usage: u}, DefaultLimits())
			}
			for _, u := range []*goai.Usage{{}, {ReasoningSet: true, CacheWrite1hSet: true}, {}} {
				if err := add(u); err != nil {
					t.Fatal(err)
				}
			}
			key := "p/m"
			if bucket == "tools" {
				key = "tool"
			}
			raw := state[bucket].(map[string]any)[key].(JSON)
			for _, name := range []string{"reasoning", "cacheWrite1h"} {
				if _, present := raw[name]; !present {
					t.Fatal("ledger lost optional zero", name)
				}
			}
			var result goai.Usage
			if err := fromObject(raw, &result, DefaultLimits()); err != nil {
				t.Fatal(err)
			}
			if !result.ReasoningSet || !result.CacheWrite1hSet || result.Reasoning != 0 || result.CacheWrite1h != 0 {
				t.Fatal("ledger round-trip lost presence")
			}
		})
	}
}
