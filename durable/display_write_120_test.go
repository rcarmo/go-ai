package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestDisplayWrite120SettlesDuringGeneration(t *testing.T) { testDisplayWrite120(t, false) }
func TestDisplayWrite120WaitsBehindQueuedReset(t *testing.T)  { testDisplayWrite120(t, true) }
func testDisplayWrite120(t *testing.T, queuedReset bool) {
	backends(t, func(t *testing.T, b backend) {
		gate := make(chan struct{})
		entered := make(chan struct{}, 1)
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			go func() {
				entered <- struct{}{}
				select {
				case <-gate:
					ch <- terminal("done")
				case <-ctx.Done():
				}
				close(ch)
			}()
			return ch
		})
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		active, err := conv.Submit(bg, Input{Content: "active"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if queuedReset {
			if err := conv.Reset(bg, "reset"); err != nil {
				t.Fatal(err)
			}
		}
		write, err := conv.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "app.display", Value: JSON{"note": "visible"}}})
		if err != nil {
			t.Fatal(err)
		}

		snap, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		want := "done"
		if queuedReset {
			want = "pending"
		}
		if snap.Submissions[write.ID()].Status != want {
			t.Fatal("display write queued")
		}
		found := false
		for _, entry := range snap.Entries {
			if entry.Kind == "app.display" {
				found = true
			}
		}
		if found == queuedReset {
			t.Fatal("display entry not immediately placed")
		}
		close(gate)
		if _, err := active.Wait(bg); err != nil {
			t.Fatal(err)
		}
		if _, err := write.Wait(bg); err != nil {
			t.Fatal(err)
		}
	})
}
