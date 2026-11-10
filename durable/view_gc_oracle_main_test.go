package durable

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"weak"
)

// harness-view.test.ts:570-600 at 81f3cd41: a live SQLite watch, min5/max10,
// first note then 200 more notes, <=10 entries and collection within three GCs.
// Go Entry is a value: these weak targets are actual pointerful entry backing
// arrays, not JS EntryRecord identities. Check both detached callback and private
// published windows so detachment cannot hide an internal retention regression.
// Other backends and explicit retention controls are native extensions.
type viewEntryWeak struct{ public, current, head weak.Pointer[Entry] }
type viewEntryHold struct{ public, current, head *Entry }
type viewEntryWitness struct {
	refs viewEntryWeak
	hold *viewEntryHold
}

//go:noinline
func runViewEntryLifetime(t *testing.T, h *Harness, c *ConversationHandle, retain bool) (*detachedConversationWatch, viewEntryWeak, *viewEntryHold) {
	t.Helper()
	w, err := detachedWatch(c, bg, transcriptBounds(5, 10))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Stop() })
	first := make(chan viewEntryWitness, 1)
	delivered := make(chan struct {
		last  ID
		count int
	}, 256)
	var once sync.Once
	if err = w.Start(func(_ context.Context, value ConversationViewCopy, _ []Operation) error {
		if len(value.Entries) == 0 {
			return nil
		}
		once.Do(func() {
			public := &value.Entries[0]
			w.mu.Lock()
			current, head := &w.current.Entries[0], &w.head.Entries[0]
			w.mu.Unlock()
			witness := viewEntryWitness{refs: viewEntryWeak{weak.Make(public), weak.Make(current), weak.Make(head)}}
			if retain {
				witness.hold = &viewEntryHold{public, current, head}
			}
			first <- witness
		})
		delivered <- struct {
			last  ID
			count int
		}{value.Entries[len(value.Entries)-1].ID, len(value.Entries)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	upstreamViewNotes(t, h, 1)
	witness := watchReceive(t, first)
	watchReceive(t, delivered)
	ids := upstreamViewNotes(t, h, 200)
	for {
		call := watchReceive(t, delivered)
		if call.count > 10 {
			t.Fatal("source window bound", call.count)
		}
		if call.last == ids[len(ids)-1] {
			break
		}
	}
	return w, witness.refs, witness.hold
}

// Weak.Value's temporary strong pointers must die before the next runtime.GC.
//
//go:noinline
func viewEntriesCollected(refs viewEntryWeak) bool {
	return refs.public.Value() == nil && refs.current.Value() == nil && refs.head.Value() == nil
}

//go:noinline
func checkViewEntriesRetained(t *testing.T, refs viewEntryWeak, hold *viewEntryHold) {
	t.Helper()
	if refs.public.Value() == nil || refs.current.Value() == nil || refs.head.Value() == nil {
		t.Fatal("positive control lost reachable entries")
	}
	runtime.KeepAlive(hold)
}

func TestMainViewUpstreamDroppedEntryLifetime(t *testing.T) {
	for _, retain := range []bool{false, true} {
		name := "source-without-control"
		if retain {
			name = "native-retention-control"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				w, refs, hold := runViewEntryLifetime(t, h, c, retain)
				if retain {
					for cycle := 0; cycle < 3; cycle++ {
						runtime.GC()
						checkViewEntriesRetained(t, refs, hold)
					}
					hold = nil
				}
				for cycle := 0; cycle < 3; cycle++ {
					runtime.GC()
					if viewEntriesCollected(refs) {
						break
					}
				}
				if !viewEntriesCollected(refs) {
					t.Fatal("dropped callback/private entry backing arrays remain reachable")
				}
				if _, closed := w.End(); closed {
					t.Fatal("watch ended during lifetime check")
				}
				// Preserve the live watch, Harness and durable records while checking GC.
				runtime.KeepAlive(w)
				runtime.KeepAlive(h)
				runtime.KeepAlive(b.store)
			})
		})
	}
}

// Source570's actual EntryRecord-object counterpart, in addition to the
// original native backing-array controls above.
func TestMainSharedViewDroppedReadonlyEntryLifetime(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		w, err := c.Watch(bg, transcriptBounds(5, 10))
		if err != nil {
			t.Fatal(err)
		}
		defer w.Stop()
		captured := make(chan weak.Pointer[ImmutableValue], 1)
		delivered := make(chan ID, 256)
		var once sync.Once
		if err := w.Start(func(_ context.Context, value ConversationView, _ []Operation) error {
			if value.Entries.Len() > 10 {
				return errors.New("window exceeded max")
			}
			first, ok := value.Entries.Index(0)
			if !ok {
				return nil
			}
			once.Do(func() { captured <- weak.Make(first.immutableNode()) })
			last, _ := value.Entries.Index(value.Entries.Len() - 1)
			id, err := immutableViewMember(t, last, "id").Number().Int64()
			if err != nil {
				return err
			}
			delivered <- ID(id)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		source525Notes(t, h.session, c.ID(), 1)
		ref := watchReceive(t, captured)
		hold := readonlyValue(ref.Value())
		watchReceive(t, delivered)
		ids := source525Notes(t, h.session, c.ID(), 200)
		for watchReceive(t, delivered) != ids[len(ids)-1] {
		}
		for cycle := 0; cycle < 3; cycle++ {
			runtime.GC()
			if ref.Value() == nil {
				t.Fatal("positive control lost retained readonly record")
			}
			runtime.KeepAlive(hold)
		}
		hold = nil
		for cycle := 0; cycle < 3; cycle++ {
			runtime.GC()
			if ref.Value() == nil {
				break
			}
		}
		if ref.Value() != nil {
			t.Fatal("dropped readonly EntryRecord remains reachable")
		}
		if _, closed := w.End(); closed {
			t.Fatal("lifetime test ended the watch")
		}
		runtime.KeepAlive(w)
		runtime.KeepAlive(h)
		runtime.KeepAlive(b.store)
	})
}
