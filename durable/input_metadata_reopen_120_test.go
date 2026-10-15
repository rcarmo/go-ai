package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestInputMetadata120QueuedReopenBeforePlacement(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{}, 1)
		var resumed atomic.Bool
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			go func() {
				if !resumed.Load() {
					entered <- struct{}{}
					<-ctx.Done()
				} else {
					ch <- terminal("done")
				}
				close(ch)
			}()
			return ch
		})
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		if _, err := conv.Submit(bg, Input{Content: "first"}); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		sub, err := conv.Submit(bg, Input{Content: "queued", Entry: &Entry{Kind: "app.queued", Data: JSON{"source": "before-restart"}, HasData: true}})
		if err != nil {
			t.Fatal(err)
		}
		snap, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range snap.Entries {
			if entry.Kind == "app.queued" {
				t.Fatal("queued input placed early")
			}
		}
		checked := false
		for _, task := range snap.Tasks {
			if task.Kind != "pi.generation" {
				continue
			}
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
				t.Fatal(err)
			}
			if cp.InputMetadata == nil {
				continue
			}
			cp.InputMetadata.Kind = "pi.forged"
			forged, err := dtoObject(cp, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			task.Checkpoint = forged
			if err := validateTaskRecord(task, h.session.limits); err == nil {
				t.Fatal("forged checkpoint metadata accepted")
			}
			checked = true
		}
		if !checked {
			t.Fatal("queued metadata checkpoint absent")
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		resumed.Store(true)
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := next.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := handle.Wait(bg); err != nil {
			t.Fatal(err)
		}
		snap, err = next.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range snap.Entries {
			if entry.Kind == "app.queued" {
				count++
				if !entry.HasData || !equalJSONValue(entry.Data, JSON{"source": "before-restart"}) || len(entry.Model) != 1 {
					t.Fatal("recovered input metadata lost", entry)
				}
			}
		}
		if count != 1 {
			t.Fatal("recovered entry count", count)
		}
	})
}
