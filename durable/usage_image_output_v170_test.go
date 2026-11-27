package durable

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUsageImageOutputV170Budget(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxMembers = 8
	value := goai.Usage{CacheWrite1hSet: true, ReasoningSet: true}
	data, err := encodeBounded(value, limits, 4096)
	if err != nil || strings.Contains(string(data), `"imageOutput"`) {
		t.Fatalf("old usage budget: %s, %v", data, err)
	}
	value.ImageOutputSet = true
	if _, err := encodeBounded(value, limits, 4096); err == nil {
		t.Fatal("an explicitly present zero did not count against the member cap")
	}
	limits.MaxMembers++
	data, err = encodeBounded(value, limits, 4096)
	if err != nil || !strings.Contains(string(data), `"imageOutput":0`) {
		t.Fatalf("explicit image output zero: %s, %v", data, err)
	}
	var decoded goai.Usage
	if err := decodeStrict(data, limits, 4096, &decoded); err != nil || !decoded.ImageOutputSet {
		t.Fatalf("strict round-trip lost image split: %+v, %v", decoded, err)
	}
}

func TestUsageImageOutputV170Aggregate(t *testing.T) {
	limits := DefaultLimits()
	models := map[string]any{}
	state := JSON{"models": models}
	for _, usage := range []goai.Usage{
		{Output: 5, TotalTokens: 5},
		{Output: 7, TotalTokens: 7, ImageOutputSet: true},
		{Output: 8, TotalTokens: 8, ImageOutput: 3, ImageOutputSet: true},
		{Output: 6, TotalTokens: 6, ImageOutput: 2, ImageOutputSet: true},
		{Output: 1, TotalTokens: 1},
	} {
		if err := addModelUsage(state, MessageReceipt{Provider: "radius", Model: "nano-banana", Usage: &usage}, limits); err != nil {
			t.Fatal(err)
		}
	}
	stored, ok := models["radius/nano-banana"].(JSON)
	if !ok {
		t.Fatalf("usage document has the wrong shape: %#v", models)
	}
	var got goai.Usage
	if err := fromObject(stored, &got, limits); err != nil {
		t.Fatal(err)
	}
	if got.ImageOutput != 5 || !got.ImageOutputSet || got.Output != 27 || got.TotalTokens != 27 {
		t.Fatalf("aggregate image output: %+v", got)
	}
}
