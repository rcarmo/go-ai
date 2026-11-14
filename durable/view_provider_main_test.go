package durable

import (
	"context"
	"strconv"
	"testing"
)

// Pinned harness/view.ts builtinDocuments and harness-view.test.ts mountedKinds
// include ProviderDoc. All three projection paths share the same kind filter.
func TestMainConversationViewProviderProjection(t *testing.T) {
	for _, mode := range []string{"hydrated", "live", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h := openHarness(t, b.store, Options{})
				conversation, e := h.Conversation(bg, 1)
				if e != nil {
					t.Fatal(e)
				}
				var provider ID
				var identity string
				create := func() {
					t.Helper()
					checkpointCommit(t, h.session, func(tx *Tx) error {
						var e error
						identity, e = ensureProviderDocument(tx, 1)
						if e != nil {
							return e
						}
						state, e := tx.current()
						if e != nil {
							return e
						}
						for id, doc := range state.Documents {
							if doc.Kind == "pi.provider" && doc.Owner == 1 && !doc.Retired {
								provider = id
							}
						}
						return nil
					})
					if provider == 0 {
						t.Fatal("provider document not created")
					}
				}
				if mode == "hydrated" {
					create()
				}
				watch, e := detachedWatch(conversation, bg)
				if e != nil {
					t.Fatal(e)
				}
				defer watch.Stop()
				initial, e := watch.Value()
				if e != nil {
					t.Fatal(e)
				}
				empty, e := detachedView(conversation, bg)
				if e != nil || empty.Entries == nil || len(empty.Entries) != 0 {
					t.Fatal("empty transcript must expose an array", empty, e)
				}
				if mode == "hydrated" {
					if initial.Docs["pi.provider"]["sessionId"] != identity {
						t.Fatal("watch omitted provider baseline", initial.Docs)
					}
					view, e := detachedView(conversation, bg)
					if e != nil || view.Docs["pi.provider"]["sessionId"] != identity {
						t.Fatal("snapshot omitted provider", view.Docs, e)
					}
					initial.Docs["pi.provider"]["sessionId"] = "caller mutation"
					retained, e := watch.Value()
					if e != nil || retained.Docs["pi.provider"]["sessionId"] != identity {
						t.Fatal("baseline aliases", retained, e)
					}
				} else {
					if _, found := initial.Docs["pi.provider"]; found {
						t.Fatal("view created absent provider")
					}
					create()
				}
				if mode == "overflow" {
					for n := 0; n < 105; n++ {
						value := strconv.Itoa(n)
						checkpointCommit(t, h.session, func(tx *Tx) error {
							doc, e := tx.documentRecord(provider)
							if e != nil {
								return e
							}
							return doc.Set(JSON{"sessionId": value})
						})
					}
					identity = "104"
				}
				replica, e := dtoObject(initial, h.session.limits)
				if e != nil {
					t.Fatal(e)
				}
				frames := make(chan ConversationViewCopy, 4)
				if e = watch.Start(func(ctx context.Context, v ConversationViewCopy, ops []Operation) error {
					// Publication data must replay, and callbacks must stay off the Session line.
					next, e := ApplyOperations(replica, ops, h.session.limits)
					if e != nil {
						return e
					}
					wire, e := dtoObject(v, h.session.limits)
					if e != nil {
						return e
					}
					if !equalJSONValue(next, wire) {
						t.Error("provider view operations do not replay", ops, "replayed", next, "wire", wire)
					}
					replica = wire
					if _, e = detachedView(conversation, ctx); e != nil {
						return e
					}
					frames <- v
					return nil
				}); e != nil {
					t.Fatal(e)
				}
				if mode != "hydrated" {
					f := watchReceive(t, frames)
					if mode == "overflow" {
						for f.Docs["pi.provider"]["sessionId"] != identity {
							f = watchReceive(t, frames)
						}
					}
					if f.Docs["pi.provider"]["sessionId"] != identity {
						t.Fatal("provider publication missing", f.Docs)
					}
					f.Docs["pi.provider"]["sessionId"] = "callback mutation"
					retained, e := watch.Value()
					if e != nil || retained.Docs["pi.provider"]["sessionId"] != identity {
						t.Fatal("callback aliases", retained, e)
					}
				}
				checkpointCommit(t, h.session, func(tx *Tx) error {
					doc, e := tx.documentRecord(provider)
					if e != nil {
						return e
					}
					return doc.Set(JSON{"sessionId": "updated"})
				})
				f := watchReceive(t, frames)
				if f.Docs["pi.provider"]["sessionId"] != "updated" {
					t.Fatal("provider update missing", f.Docs)
				}
				checkpointCommit(t, h.session, func(tx *Tx) error {
					doc, e := tx.documentRecord(provider)
					if e != nil {
						return e
					}
					return doc.Retire()
				})
				f = watchReceive(t, frames)
				if _, found := f.Docs["pi.provider"]; found {
					t.Fatal("retired provider still mounted", f.Docs)
				}
				if current, e := detachedView(conversation, bg); e != nil || current.Docs["pi.provider"] != nil {
					t.Fatal("retired provider snapshot", current.Docs, e)
				}
			})
		})
	}
}
