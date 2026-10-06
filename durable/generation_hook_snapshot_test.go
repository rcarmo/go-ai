package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationResponseHooksUsePhaseRegistryAndRefreshNextRequest(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		registry := NewRegistry()
		var oldHooks, newHooks, requests atomic.Int64
		old := &Extension{Name: "phase-hooks", Hooks: GenerationHooks{AfterResponse: func(context.Context, MessageReceipt, *HookAPI) error { oldHooks.Add(1); return nil }}}
		replacement := &Extension{Name: "phase-hooks", Hooks: GenerationHooks{AfterResponse: func(context.Context, MessageReceipt, *HookAPI) error { newHooks.Add(1); return nil }}}
		if err := registry.Install(old); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
					close(ch)
					return ch
				}
			}
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation := root(t, h, ref)
		first, err := conversation.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err := registry.Install(replacement); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		waitSubmission(t, first)
		if oldHooks.Load() != 1 || newHooks.Load() != 0 {
			t.Fatal("inflight response hooks retargeted", oldHooks.Load(), newHooks.Load())
		}
		second, err := conversation.Submit(bg, Input{Content: "second"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, second)
		if oldHooks.Load() != 1 || newHooks.Load() != 1 || requests.Load() != 2 {
			t.Fatal("new phase registry not refreshed", oldHooks.Load(), newHooks.Load(), requests.Load())
		}
	})
}
