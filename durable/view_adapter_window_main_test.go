package durable

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMainTranscriptAdapterPublicationWindow(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var provider ID
				for id, doc := range state.Documents {
					if doc.Kind == "pi.provider" && doc.Owner == 1 {
						provider = id
					}
				}
				saved := h.session.store
				adapter := &mainDefinitionProjection{nativeStorage: saved.(nativeStorage), id: provider, origin: "baseline"}
				defer h.session.taskBookkeeping(func() { h.session.store = saved })
				h.session.taskBookkeeping(func() { h.session.store = adapter })
				future := ID(MaxID)
				bounds := transcriptBounds(3, 6)
				bounds.From = &future
				watch, err := detachedWatch(c, bg, bounds)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				_, raw, err := h.session.SubscribeCommits(bg)
				if err != nil {
					t.Fatal(err)
				}
				defer raw.Stop()
				initial, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				defer once.Do(func() { close(release) })
				type delivery struct {
					value ConversationViewCopy
					ops   []Operation
				}
				frames := make(chan delivery, 120)
				calls := 0
				start := func() {
					if err := watch.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
						calls++
						if blocked && calls == 1 {
							close(initial)
							<-release
						}
						frames <- delivery{value, ops}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				expected := map[ID]ConversationViewCopy{}
				ids := []ID{}
				previous := ID(0)
				appendOne := func(n int) {
					h.session.taskBookkeeping(func() { adapter.origin = fmt.Sprint(n) })
					id := transcriptNotes(t, h, 1)[0]
					ids = append(ids, id)
					if len(ids) > 6 {
						previous = ids[len(ids)-4]
						ids = ids[len(ids)-3:]
					}
					entries := make([]Entry, len(ids))
					for i, id := range ids {
						entries[i].ID = id
					}
					expected[id] = ConversationViewCopy{Entries: entries, Before: previous}
				}
				if blocked {
					start()
					appendOne(0)
					select {
					case <-initial:
					case <-time.After(3 * time.Second):
						t.Fatal("missing initial call")
					}
				}
				for n := 1; n <= 105; n++ {
					appendOne(n)
				}
				raw.mu.Lock()
				rawReset := len(raw.pending) > 0 && raw.pending[0].Snapshot != nil
				raw.mu.Unlock()
				if !rawReset {
					t.Fatal("raw subscription lost snapshot reset")
				}
				if blocked {
					once.Do(func() { close(release) })
				} else {
					start()
				}
				resets := 0
				for {
					select {
					case frame := <-frames:
						tail := frame.value.Entries[len(frame.value.Entries)-1].ID
						want := expected[tail]
						wantIDs := make([]ID, len(want.Entries))
						for i, e := range want.Entries {
							wantIDs[i] = e.ID
						}
						transcriptIDs(t, frame.value, wantIDs, want.Before)
						if frame.ops[0][0] == "r" {
							resets++
							if frame.value.Docs["pi.provider"]["origin"] != "101" {
								t.Fatal("reset lost authoritative projection", frame.value.Docs)
							}
						}
						if tail == ids[len(ids)-1] {
							if resets != 1 {
								t.Fatal("wrong reset count", resets)
							}
							if enabled, failed := watch.projectionStatus(); enabled || failed {
								t.Fatal("adapter layout became a native witness", enabled, failed)
							}
							return
						}
					case <-time.After(3 * time.Second):
						end, _ := watch.End()
						t.Fatal("missing adapter window", end)
					}
				}
			})
		})
	}
}

func TestMainTranscriptAdapterResetPreparation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				ids := transcriptNotes(t, h, 4)
				bounds := transcriptBounds(3, 6)
				bounds.From = &ids[0]
				watch, err := detachedWatch(c, bg, bounds)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				baseline, err := watch.Value()
				if err != nil {
					t.Fatal(err)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if missing {
					delete(state.Entries, ids[0])
				} else {
					e := state.Entries[ids[0]]
					e.Value = JSON{"origin": "authoritative-entry"}
					state.Entries[e.ID] = e
				}
				var provider ID
				for id, doc := range state.Documents {
					if doc.Kind == "pi.provider" {
						provider = id
						doc.Value = JSON{"origin": "authoritative-doc"}
						state.Documents[id] = doc
					}
				}
				ready := make(chan struct{})
				next, frame, size, err := watch.prepareAuthoritativeReset(bg, CommitPublication{}, state, ready)
				if err != nil {
					t.Fatal(err)
				}
				if size <= 0 || frame.ready != ready {
					t.Fatal("reset lost accounting/readiness")
				}
				want := ids
				if missing {
					want = ids[1:]
				}
				transcriptIDs(t, next, want, 0)
				if next.Docs["pi.provider"]["origin"] != "authoritative-doc" {
					t.Fatal("stale document body")
				}
				if !missing && next.Entries[0].Value["origin"] != "authoritative-entry" {
					t.Fatal("stale retained entry body")
				}
				// Preparation and caller mutation cannot alter the witness/current value.
				state.Documents[provider].Value["origin"] = "borrowed"
				if !missing {
					state.Entries[ids[0]].Value["origin"] = "borrowed"
				}
				if next.Docs["pi.provider"]["origin"] != "authoritative-doc" || (!missing && next.Entries[0].Value["origin"] != "authoritative-entry") {
					t.Fatal("reset borrowed snapshot payloads")
				}
				watch.mu.Lock()
				unchanged := len(watch.head.Entries) == 4 && !watch.projectionDisabled
				watch.mu.Unlock()
				if !unchanged {
					t.Fatal("abandoned reset preparation changed witness")
				}
				value, err := watch.Value()
				if err != nil {
					t.Fatal(err)
				}
				oldBody, _ := dtoObject(baseline, h.session.limits)
				currentBody, _ := dtoObject(value, h.session.limits)
				if !equalJSONValue(oldBody, currentBody) {
					t.Fatal("preparation altered delivered value")
				}
				applied, err := ApplyOperations(oldBody, frame.conversation.ops, h.session.limits)
				wantBody, _ := dtoObject(next, h.session.limits)
				if err != nil || !equalJSONValue(applied, wantBody) {
					t.Fatal("reset replay differs", err)
				}
			})
		})
	}
}
