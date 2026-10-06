package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestHarnessInspectListsOnlyUnsettledSubmissionsDetachedWithoutEffects(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			close(entered)
			go func() { <-release; ch <- terminal("answer"); close(ch) }()
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		passive := root(t, h, ref)
		write, err := passive.Submit(bg, Input{Type: "write", Content: "already settled"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, write)
		sub, err := conversation.Submit(bg, Input{Content: "working"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		view, err := h.Inspect(bg)
		if err != nil {
			t.Fatal(err)
		}
		if view.Scheduling != "running" || len(view.Submissions) != 1 || view.Submissions[0].ID != sub.ID() || len(view.Tasks) != 1 || view.Tasks[0].Record.Kind != "pi.generation" || view.Tasks[0].Kind != "running" {
			t.Fatal(view)
		}
		view.Submissions[0].Value["content"] = "mutated"
		view.Tasks[0].Record.State.Checkpoint["phase"] = "mutated"
		again, err := h.Inspect(bg)
		if err != nil {
			t.Fatal(err)
		}
		after, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if before.Seq != after.Seq || again.Submissions[0].Value["content"] == "mutated" || again.Tasks[0].Record.State.Checkpoint["phase"] == "mutated" {
			t.Fatal("inspection changed state", again)
		}
		close(release)
		waitSubmission(t, sub)
		view, err = h.Inspect(bg)
		if err != nil || len(view.Submissions) != 0 || len(view.Tasks) != 0 {
			t.Fatal(view, err)
		}
	})
}
