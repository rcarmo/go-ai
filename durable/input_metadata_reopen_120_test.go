package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestInputMetadata120QueuedReopenBeforePlacement(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
		metadata := JSON{"source": "before-restart"}
		entry := &Entry{Kind: "app.queued", Data: metadata, HasData: true}
		sub, err := conv.Submit(bg, Input{Content: "queued", Entry: entry})
		if err != nil {
			t.Fatal(err)
		}
		metadata["source"] = "caller mutated"
		entry.Kind = "pi.forged"
		queued, exists, err := h.SnapshotDefinition(bg, InboxDoc(), conv.ID(), nil)
		if err != nil || !exists {
			t.Fatal("queued inbox", exists, err)
		}
		wantInbox := JSON{"items": []any{JSON{"id": float64(sub.ID()), "mode": "followUp", "content": "queued", "entry": JSON{"kind": "app.queued", "data": JSON{"source": "before-restart"}}}}}
		if !equalJSONValue(queued.Copy(), wantInbox) {
			t.Fatal("queued metadata must be owned by the real inbox", queued.Copy())
		}
		before, err := conv.Entries(bg, EntryQuery{}, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range before.Items {
			if entry.Kind == "app.queued" {
				t.Fatal("queued input placed early")
			}
		}
		if _, err := conv.Submit(bg, Input{Content: "forged", Entry: &Entry{Kind: "pi.forged"}}); err == nil {
			t.Fatal("reserved input kind accepted")
		}
		after, exists, err := h.SnapshotDefinition(bg, InboxDoc(), conv.ID(), nil)
		if err != nil || !exists || after != queued {
			t.Fatal("rejected input changed the shared inbox", exists, err)
		}
		afterEntries, err := conv.Entries(bg, EntryQuery{}, 100, nil)
		if err != nil || !reflect.DeepEqual(before, afterEntries) {
			t.Fatal("rejected metadata wrote an entry", err)
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
		reopened, err := next.Conversation(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		history, err := reopened.Entries(bg, EntryQuery{}, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range history.Items {
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
