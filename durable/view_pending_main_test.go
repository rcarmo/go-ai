package durable

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// view.ts mounts Chord's JSON watch, not its 100-value native watch.
// Only pending calls fold; an in-flight call retains its own delivery.
func TestMainConversationPendingDelivery(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked=%v", blocked), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				watch, err := c.Watch(bg)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				baseline, err := watch.Value()
				if err != nil {
					t.Fatal(err)
				}
				state, raw, err := h.session.SubscribeCommits(bg)
				if err != nil {
					t.Fatal(err)
				}
				defer raw.Stop()
				var liveID ID
				for _, doc := range state.Documents {
					if doc.Kind == "pi.live" && doc.Owner == c.ID() && !doc.Retired {
						liveID = doc.ID
					}
				}
				if liveID == 0 {
					t.Fatal("missing live document")
				}
				initial, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				defer once.Do(func() { close(release) })
				type key struct{}
				type delivery struct {
					value ConversationView
					ops   []Operation
					ctx   context.Context
				}
				got := make(chan delivery, 110)
				calls := 0
				start := func() {
					if err := watch.Start(func(ctx context.Context, value ConversationView, ops []Operation) error {
						if _, err := c.View(bg); err != nil {
							return err
						} // callback stays off-line
						calls++
						if blocked && calls == 1 {
							close(initial)
							<-release
						}
						got <- delivery{value, ops, ctx}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				commit := func(n int) {
					ctx, cancel := context.WithCancel(context.WithValue(bg, key{}, n))
					defer cancel()
					if _, err := h.session.Commit(ctx, func(tx *Tx) error {
						live, err := tx.Document(liveID)
						if err != nil {
							return err
						}
						return live.ApplyOperations([]Operation{{"s", []any{"fold"}, n}})
					}); err != nil {
						t.Fatal(err)
					}
				}
				if blocked {
					start()
					commit(0)
					select {
					case <-initial:
					case <-time.After(3 * time.Second):
						t.Fatal("missing active callback")
					}
				}
				for n := 1; n <= 102; n++ {
					commit(n)
				}
				raw.mu.Lock()
				rawReset := len(raw.pending) > 0 && raw.pending[0].Snapshot != nil
				raw.mu.Unlock()
				if !rawReset {
					t.Fatal("raw commit subscription lost its 100-frame reset")
				}
				if blocked {
					once.Do(func() { close(release) })
					select {
					case first := <-got:
						assertConversationOperations(t, first.ops, []Operation{{"s", []any{"docs", "pi.live", "fold"}, 0}}, h.session.limits)
					case <-time.After(3 * time.Second):
						t.Fatal("missing original call")
					}
				} else {
					start()
				}
				select {
				case frame := <-got:
					assertConversationOperations(t, frame.ops, []Operation{{"s", []any{"docs", "pi.live", "fold"}, 102}}, h.session.limits)
					if frame.ctx.Value(key{}) != 102 || frame.ctx.Err() != nil {
						t.Fatal("folded context", frame.ctx.Value(key{}), frame.ctx.Err())
					}
					body, _ := dtoObject(baseline, h.session.limits)
					applied, err := ApplyOperations(body, frame.ops, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					want, _ := dtoObject(frame.value, h.session.limits)
					if !equalJSONValue(applied, want) {
						t.Fatal("folded replay differs")
					}
					frame.value.Docs["pi.live"]["fold"] = "borrowed"
					frame.ops[0][2] = "borrowed"
					current, err := watch.Value()
					if err != nil || fmt.Sprint(current.Docs["pi.live"]["fold"]) != "102" {
						t.Fatal("callback borrowed private value", current, err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("missing folded delivery")
				}
				commit(103)
				select {
				case frame := <-got:
					assertConversationOperations(t, frame.ops, []Operation{{"s", []any{"docs", "pi.live", "fold"}, 103}}, h.session.limits)
				case <-time.After(3 * time.Second):
					t.Fatal("missing successor")
				}
			})
		})
	}
}
