package durable

import (
	"context"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSessionUsageTotalsDetachedAndForkStartsEmpty(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "root"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var tail ID
		for id, entry := range state.Entries {
			if entry.Conversation == conversation.ID() && id > tail {
				tail = id
			}
		}
		fork, err := conversation.Fork(bg, tail)
		if err != nil {
			t.Fatal(err)
		}
		usage, err := fork.Usage(bg)
		if err != nil || len(usage.Models) != 0 {
			t.Fatal("fork copied spend", usage, err)
		}
		child, err := h.CreateConversation(bg, AgentChange{Model: ref})
		if err != nil {
			t.Fatal(err)
		}
		next, err := child.Submit(bg, Input{Content: "child"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, next)
		_, err = h.session.Commit(bg, func(tx *Tx) (any, error) {
			usage, err := builtin(tx, child.ID(), "pi.usage")
			if err != nil {
				return nil, err
			}
			return nil, usage.Update(func(value JSON) error {
				return addToolUsage(value, "__proto__", &goai.Usage{Input: 2, TotalTokens: 2, Cost: goai.CostBreakdown{Total: 0.5}}, tx.limits)
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		total, err := h.Usage(bg)
		if err != nil {
			t.Fatal(err)
		}
		key := string(ref.Provider) + "/" + ref.ID
		if total.Models[key].TotalTokens != 10 || total.Tools["__proto__"].TotalTokens != 2 || total.Tools["__proto__"].Cost.Total != 0.5 {
			t.Fatal("usage totals", total)
		}
		total.Models[key] = goai.Usage{TotalTokens: 999}
		again, err := h.Usage(bg)
		if err != nil || again.Models[key].TotalTokens != 10 {
			t.Fatal("usage map escaped", again, err)
		}
		own, err := conversation.Usage(bg)
		if err != nil || own.Models[key].TotalTokens != 5 || len(own.Tools) != 0 {
			t.Fatal("conversation total", own, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		persisted, err := reopened.Usage(bg)
		if err != nil || persisted.Models[key].TotalTokens != 10 || persisted.Tools["__proto__"].TotalTokens != 2 {
			t.Fatal("usage reopen", persisted, err)
		}
	})
}

func TestUsageImageOutputBudget(t *testing.T) {
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

func TestUsageImageOutputAggregate(t *testing.T) {
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
