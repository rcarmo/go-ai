package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestSessionUsageTotalsDetachedAndForkStartsEmpty(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			usage, err := builtin(tx, child.ID(), "pi.usage")
			if err != nil {
				return err
			}
			return usage.Update(func(value JSON) error {
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
