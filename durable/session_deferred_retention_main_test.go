package durable

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"weak"
)

// c9f4c7b9 source600: partial native translation. Held ownership and zero idle
// migrated-value cache replace neither Source's cached/attached tracker counts
// nor its public snapshot identity. Those assertions remain explicitly unported.
func TestMainSessionDeferredRetentionZeroOwnedValue(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := openMainDeadlineSession(t, b, nil, 0)
		old, _ := mainDeferredLifecycleDefinitions(t)
		if _, err := f.session.Commit(bg, func(*Tx) (any, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		f.session.taskBookkeeping(func() {
			if len(f.session.definitionCache) != 0 || len(f.session.heldDocuments) != 0 {
				t.Fatal("initial idle cache/holds")
			}
		})
		before := mainDeferredSnapshot(t, b.store).Seq
		mainDeferredLifecycleAppend(t, f.session, old, 1, "held")
		if _, err := f.session.Commit(bg, func(*Tx) (any, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		if value, ok, err := detachedDefinitionSnapshot(f.session, bg, old, 1, nil); err != nil || !ok || !equalJSONValue(value, JSON{"text": "held"}) {
			t.Fatal(value, ok, err)
		}
		f.session.taskBookkeeping(func() {
			if len(f.session.heldDocuments) != 1 || !equalJSONValue(f.session.heldDocuments[f.doc].record.Value, JSON{"text": "held"}) {
				t.Fatal("zero retention lost owned hold")
			}
			if len(f.session.definitionCache) != 0 {
				t.Fatal("native idle migration cache retained at zero")
			}
		})
		if raw := mainDeferredSnapshot(t, b.store); raw.Seq != before || !equalJSONValue(raw.Documents[f.doc].Value, JSON{"text": ""}) {
			t.Fatal("retention cleanup stored hold early", raw)
		}
		f.fire(t)
		if _, err := f.session.Commit(bg, func(*Tx) (any, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		f.session.taskBookkeeping(func() {
			if len(f.session.heldDocuments) != 0 || len(f.session.definitionCache) != 0 || f.session.memoryImage != nil {
				t.Fatal("stored hold retained process-local roots")
			}
		})
		if value, ok, err := detachedDefinitionSnapshot(f.session, bg, old, 1, nil); err != nil || !ok || !equalJSONValue(value, JSON{"text": "held"}) {
			t.Fatal("stored cold read", value, ok, err)
		}
	})
}

// Source613 uses a WeakRef to the shared public Text value. Native snapshots
// detach maps, which are not weak-addressable: this is a separately credited
// lifetime control for the ACTUAL pointerful owned overlay, not a proxy box and
// not a claim that the Source public-identity assertion has been ported.
func TestMainSessionDeferredHeldOverlayLifetime(t *testing.T) {
	for _, retain := range []bool{false, true} {
		name := "without-extra-root"
		if retain {
			name = "native-retention-control"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := openMainDeadlineSession(t, b, nil, 0)
				old, _ := mainDeferredLifecycleDefinitions(t)
				text := strings.Repeat("x", 1000)
				mainDeferredLifecycleAppend(t, f.session, old, 1, text)
				ref, extra := mainDeferredOverlayWitness(t, f.session, old, retain)
				for round := 0; round < 5; round++ {
					runtime.GC()
					if mainDeferredOverlayCollected(ref) {
						t.Fatal("Session lost a held overlay root")
					}
				}
				f.fire(t)
				if _, err := f.session.Commit(bg, func(*Tx) (any, error) { return nil, nil }); err != nil {
					t.Fatal(err)
				}
				// Source clears its recorded stored/observed arrays. Native stored
				// subscription is unstarted; remove its recorded frames, not the observer.
				f.stored.mu.Lock()
				f.stored.pending = nil
				f.stored.mu.Unlock()
				if retain {
					for round := 0; round < 5; round++ {
						runtime.GC()
						if mainDeferredOverlayCollected(ref) {
							t.Fatal("positive control lost its real overlay")
						}
						runtime.KeepAlive(extra)
					}
					extra = nil
				}
				for round := 0; round < 5; round++ {
					runtime.GC()
				}
				if !mainDeferredOverlayCollected(ref) {
					t.Fatal("stored overlay remains reachable")
				}
				if value, ok, err := detachedDefinitionSnapshot(f.session, bg, old, 1, nil); err != nil || !ok || !equalJSONValue(value, JSON{"text": text}) {
					t.Fatal("collection changed durable value", value, ok, err)
				}
				runtime.KeepAlive(f.session)
				runtime.KeepAlive(b.store)
			})
		})
	}
}

// c9f4c7b9:638. The same a/b/c progression, first stored a, failure and reopened
// a. A native asynchronous document watch replaces followCommits; receiving its
// real frames is an explicit API adaptation, not synchronous-observer proof.
func TestMainSessionDeferredCrashLastStoredMemoryState(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := openMainDeadlineSession(t, b, nil)
		old, _ := mainDeferredLifecycleDefinitions(t)
		watch, ok, err := f.session.WatchDefinition(bg, old, 1, nil)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		defer watch.Stop()
		values := make(chan string, 3)
		if err := startDetachedDocumentValues(watch, func(_ context.Context, value JSON, _ []Operation) error { values <- value["text"].(string); return nil }); err != nil {
			t.Fatal(err)
		}
		states := []string{}
		for _, part := range []string{"a", "b", "c"} {
			mainDeferredLifecycleAppend(t, f.session, old, 1, part)
			states = append(states, watchReceive(t, values))
			if part == "a" {
				f.fire(t)
			}
		}
		failure := errors.New("crash")
		_ = f.session.backendResult(bg, failure, true)
		if end, err := f.session.Closed(bg); err != nil || end.Reason != "failed" || !errors.Is(end.Error, failure) {
			t.Fatal(end, err)
		}
		reopened := utf16ReopenStore(t, b.store)
		seedRootConversation(t, reopened)
		session, err := OpenSession(reopened)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		value, ok, err := detachedDefinitionSnapshot(session, bg, old, 1, nil)
		if err != nil || !ok || !equalJSONValue(value, JSON{"text": "a"}) {
			t.Fatal("recovery did not use stored prefix", value, ok, err)
		}
		found := false
		for _, state := range states {
			found = found || state == value["text"]
		}
		if !found {
			t.Fatal("recovered state was never in memory", states, value)
		}
		session.taskBookkeeping(func() {
			if len(session.heldDocuments) != 0 {
				t.Fatal("recovery restored volatile holds")
			}
		})
	})
}

// Use the Session's actual heap object containing the owned memory image. No
// caller-created stand-in can establish lifetime of an internal allocation.
//
//go:noinline
func mainDeferredOverlayWitness(t *testing.T, s *Session, def *DocumentDefinition, retain bool) (weak.Pointer[Snapshot], *Snapshot) {
	t.Helper()
	if _, ok, err := detachedDefinitionSnapshot(s, bg, def, 1, nil); err != nil || !ok {
		t.Fatal(ok, err)
	}
	var ref weak.Pointer[Snapshot]
	var extra *Snapshot
	s.taskBookkeeping(func() {
		if s.memoryImage == nil {
			t.Fatal("no owned overlay")
		}
		ref = weak.Make(s.memoryImage)
		if retain {
			extra = s.memoryImage
		}
	})
	return ref, extra
}

// Weak.Value's temporary strong pointer must die before the next GC round.
//
//go:noinline
func mainDeferredOverlayCollected(ref weak.Pointer[Snapshot]) bool { return ref.Value() == nil }
