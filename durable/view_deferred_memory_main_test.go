package durable

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// Source-derived prerequisites, NOT a port of the entire gated generation test.
// Official v1.5.0 f9a26365: harness/view.ts:141-156,325-391 advances on memory
// adoption, using own changes, and harness-view.test.ts:128-158 replays frames.
// A standalone Session behind a ConversationHandle exercises the actual Watch
// path without enabling deferred writes in the as-yet unported Harness scheduler.
func mainMemoryConversation(t *testing.T, b sqliteFixture) (*mainDeadlineSession, *ConversationHandle, ID) {
	t.Helper()
	f := openMainDeadlineSession(t, b, nil)
	if _, err := f.session.Commit(bg, func(tx *Tx) (any, error) { return nil, initializeBuiltins(tx, 1) }); err != nil {
		t.Fatal(err)
	}
	state, err := f.session.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var live ID
	for id, doc := range state.Documents {
		if doc.Owner == 1 && doc.Kind == "pi.live" {
			live = id
		}
	}
	if live == 0 {
		t.Fatal("missing live document")
	}
	return f, &ConversationHandle{h: &Harness{session: f.session}, id: 1}, live
}

func mainMemoryLive(t *testing.T, f *mainDeadlineSession, live ID, ops []Operation, withinMs ...float64) {
	t.Helper()
	if len(withinMs) == 0 {
		withinMs = []float64{1e6}
	}
	if _, err := f.session.commitPersistence(bg, func(tx *Tx) error {
		h, err := tx.documentRecord(live)
		if err != nil {
			return err
		}
		return h.ApplyOperations(ops)
	}, true, 0, true, withinMs...); err != nil {
		t.Fatal(err)
	}
}

// Wait for actual subscription work, not a guessed asynchronous callback delay.
func mainMemoryViewDrained(t *testing.T, watch *detachedConversationWatch) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		watch.sub.mu.Lock()
		idle := !watch.sub.running && len(watch.sub.pending) == 0
		watch.sub.mu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("conversation subscription did not drain")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestMainSessionConversationMemoryDelivery(t *testing.T) {
	for _, flush := range []string{"timer", "read-dependent", "changed-dependent"} {
		t.Run(flush, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f, c, live := mainMemoryConversation(t, b)
				watch, err := detachedWatch(c, bg)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				initial, err := watch.Value()
				if err != nil {
					t.Fatal(err)
				}
				frames := make(chan upstreamWindowFrame, 8)
				if err = watch.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
					frames <- upstreamWindowFrame{value, ops}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				var stored []sessionPublication
				if _, err = f.session.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p sessionPublication) error {
					stored = append(stored, p)
					return nil
				}, end: func(WatchEnd) error { return nil }}); err != nil {
					t.Fatal(err)
				}
				mainMemoryLive(t, f, live, []Operation{{"s", []any{"generation"}, JSON{"attempt": 1}}})
				memory := watchReceive(t, frames)
				wantMemory, err := ownOperations([]Operation{{"s", []any{"docs", "pi.live", "generation"}, JSON{"attempt": 1}}}, f.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(memory.ops, wantMemory) {
					t.Error("memory operation shape", memory.ops)
				}
				if len(stored) != 0 {
					t.Fatal("memory presentation reached stored observers")
				}
				disk, err := b.store.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if len(disk.Documents[live].Value) != 0 {
					t.Fatal("attempt was stored before its memory frame", disk.Documents[live].Value)
				}
				late, err := detachedWatch(c, bg)
				if err != nil {
					t.Fatal(err)
				}
				lateValue, err := late.Value()
				late.Stop()
				if err != nil || !reflect.DeepEqual(lateValue, memory.value) {
					t.Fatal("acquisition during hold lost memory revision", lateValue, err)
				}
				seen := []upstreamWindowFrame{memory}
				if flush == "timer" {
					// Tighten the deadline with a second memory revision, then
					// fire the real captured persistence timer.
					if _, err = f.session.commitPersistence(bg, func(tx *Tx) error {
						h, err := tx.documentRecord(live)
						if err != nil {
							return err
						}
						return h.ApplyOperations([]Operation{{"s", []any{"generation"}, JSON{"attempt": 2}}})
					}, true, 0, true, 10); err != nil {
						t.Fatal(err)
					}
					seen = append(seen, watchReceive(t, frames))
					f.fire(t)
				} else {
					if _, err = f.session.Commit(bg, func(tx *Tx) (any, error) {
						h, err := tx.documentRecord(live) // Depends on the held presentation.
						if err != nil {
							return nil, err
						}
						if _, err = h.Get(); err != nil {
							return nil, err
						}
						if flush == "changed-dependent" {
							if err = h.ApplyOperations([]Operation{{"d", []any{"generation"}}}); err != nil {
								return nil, err
							}
						}
						id, err := tx.mintID()
						if err != nil {
							return nil, err
						}
						return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "test.note", Value: JSON{}})
					}); err != nil {
						t.Fatal(err)
					}
					own := watchReceive(t, frames)
					want := []Operation{}
					if flush == "changed-dependent" {
						want = append(want, Operation{"d", []any{"docs", "pi.live", "generation"}})
					}
					want = append(want, Operation{"p", []any{"entries"}, 0, 0, []any{}})
					want, err = ownOperations(want, f.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					if len(own.ops) != len(want) {
						t.Fatal("dependent flush replayed held document operations", own.ops)
					}
					for i, op := range want[:len(want)-1] {
						if !reflect.DeepEqual(own.ops[i], op) {
							t.Error("own document operation", own.ops)
						}
					}
					if len(own.ops[len(own.ops)-1]) != 5 || !reflect.DeepEqual(own.ops[len(own.ops)-1][:4], want[len(want)-1][:4]) || len(own.value.Entries) != 1 {
						t.Error("missing own entry append", own.ops)
					}
					seen = append(seen, own)
				}
				mainMemoryViewDrained(t, watch)
				if len(frames) != 0 || len(stored) != 1 {
					t.Fatal("flush duplicated memory delivery or storage", len(frames), len(stored))
				}
				last := replayUpstreamWindow(t, f.session, initial, seen)
				fresh, err := detachedView(c, bg)
				if err != nil || !reflect.DeepEqual(last, fresh) {
					t.Fatal("replay differs from current memory view", last, fresh, err)
				}
				disk, err = b.store.Snapshot(bg)
				if err != nil || !reflect.DeepEqual(disk.Documents[live].Value, last.Docs["pi.live"]) {
					t.Fatal("stored final value differs", disk.Documents[live].Value, err)
				}
			})
		})
	}
}

func TestMainSessionConversationMemoryBacklog(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f, c, live := mainMemoryConversation(t, b)
		// A string append detects accidental reapplication of already seen holds.
		if _, err := f.session.Commit(bg, func(tx *Tx) (any, error) {
			h, err := tx.documentRecord(live)
			if err != nil {
				return nil, err
			}
			return nil, h.ApplyOperations([]Operation{{"s", []any{"text"}, ""}})
		}); err != nil {
			t.Fatal(err)
		}
		watch, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		initial, err := watch.Value()
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"a", "b", "c"} {
			mainMemoryLive(t, f, live, []Operation{{"a", []any{"text"}, text}}, 10)
		}
		// Store every hold without a new own change. No callback has run yet.
		f.fire(t)
		disk, err := b.store.Snapshot(bg)
		if err != nil || disk.Documents[live].Value["text"] != "abc" {
			t.Fatal("timer did not store the pending memory value", disk.Documents[live].Value, err)
		}
		frames := make(chan upstreamWindowFrame, 8)
		if err = watch.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
			frames <- upstreamWindowFrame{value, ops}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		frame := watchReceive(t, frames)
		mainMemoryViewDrained(t, watch)
		if len(frames) != 0 || frame.value.Docs["pi.live"]["text"] != "abc" {
			t.Fatal("backlog did not coalesce to exactly one abc revision", frame.value, len(frames))
		}
		if !reflect.DeepEqual(frame.ops, []Operation{{"a", []any{"docs", "pi.live", "text"}, "abc"}}) {
			t.Error("folded memory operation shape", frame.ops)
		}
		replayUpstreamWindow(t, f.session, initial, []upstreamWindowFrame{frame})
	})
}
