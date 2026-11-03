package durable

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Source-derived from followCommits' Set identity/copied iteration and
// #publish/#notifyHosts: locals execute before the queued host callbacks.
// An object pointer represents Go callback identity; no unsafe function keys.
func TestMainSessionLocalListenerSetAndHostOrder(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, add := mainSyncCounterSession(t, b)
		var mu sync.Mutex
		var calls []string
		note := func(v string) { mu.Lock(); calls = append(calls, v); mu.Unlock() }
		later := &sessionCommitListener{callback: func(context.Context, sessionObservedCommit) error { note("later"); return nil }}
		var detachLater func()
		first := &sessionCommitListener{callback: func(context.Context, sessionObservedCommit) error { note("first"); detachLater(); return nil }}
		detach1, err := s.followSessionCommits(first)
		if err != nil {
			t.Fatal(err)
		}
		defer detach1()
		detach2, err := s.followSessionCommits(first)
		if err != nil {
			t.Fatal(err)
		}
		defer detach2()
		detachLater, err = s.followSessionCommits(later)
		if err != nil {
			t.Fatal(err)
		}
		defer detachLater()
		if _, err = s.observeStoredCommits(sessionCommitObserver{advance: func(context.Context, CommitPublication) error { note("host"); return nil }, end: func(WatchEnd) error { return nil }}); err != nil {
			t.Fatal(err)
		}
		if err = add(); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := append([]string(nil), calls...)
		mu.Unlock()
		if !reflect.DeepEqual(got, []string{"first", "later", "host"}) {
			t.Error("copied Set/host order", got)
		}
		detach1()
		detach3, err := s.followSessionCommits(first)
		if err != nil {
			t.Fatal(err)
		}
		defer detach3()
		// A Source Set deletion targets the function, not the old registration:
		// this second old detacher also removes a re-added identical function.
		detach2()
		if err = add(); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got = append([]string(nil), calls...)
		mu.Unlock()
		if !reflect.DeepEqual(got, []string{"first", "later", "host", "host"}) {
			t.Error("identity/re-add/delete", got)
		}
	})
}

// Source-derived: unlike the literal564 fire after an immediate store, this
// timer really stores a hold. Only host observers see that storage publication.
func TestMainSessionLocalTimerNoDuplicate(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := openMainDeadlineSession(t, b, nil)
		var mu sync.Mutex
		var kinds []string
		var stored []CommitPublication
		detach, err := f.session.followSessionCommits(&sessionCommitListener{callback: func(_ context.Context, e sessionObservedCommit) error {
			mu.Lock()
			kinds = append(kinds, e.kind)
			mu.Unlock()
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer detach()
		if _, err = f.session.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p CommitPublication) error {
			mu.Lock()
			stored = append(stored, p)
			mu.Unlock()
			return nil
		}, end: func(WatchEnd) error { return nil }}); err != nil {
			t.Fatal(err)
		}
		if err = f.append(f.doc, "held"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		before := append([]string(nil), kinds...)
		mu.Unlock()
		if !reflect.DeepEqual(before, []string{"memory"}) {
			t.Error("memory adoption not observed synchronously", before)
		}
		f.fire(t)
		mu.Lock()
		defer mu.Unlock()
		if !reflect.DeepEqual(kinds, []string{"memory"}) || len(stored) != 1 {
			t.Error("timer duplicated local delivery or omitted host publication", kinds, len(stored))
		}
	})
}

// Source-derived from writer.ts: own-listener failures do not reject the
// already-published commit, all copied peers run, THEN the Session fails with
// the first original error. A later store must never rescue a failed hold.
func TestMainSessionLocalListenerFatalAfterPublication(t *testing.T) {
	for _, memory := range []bool{false, true} {
		name := "stored"
		if memory {
			name = "memory"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := openMainDeadlineSession(t, b, nil)
				s := f.session
				first, second := errors.New("first local bug"), errors.New("second local bug")
				var mu sync.Mutex
				var calls []string
				var hostEnd WatchEnd
				note := func(v string) { mu.Lock(); calls = append(calls, v); mu.Unlock() }
				for i := 0; i < 3; i++ {
					index := i
					detach, err := s.followSessionCommits(&sessionCommitListener{callback: func(_ context.Context, e sessionObservedCommit) error {
						switch index {
						case 0:
							note("first")
							return first
						case 1:
							note("second")
							panic(second)
						default:
							note("healthy")
							return nil
						}
					}})
					if err != nil {
						t.Fatal(err)
					}
					defer detach()
				}
				reports := 0
				if _, err := s.observeStoredCommits(sessionCommitObserver{advance: func(context.Context, CommitPublication) error { note("host"); return nil }, end: func(e WatchEnd) error { mu.Lock(); hostEnd = e; calls = append(calls, "end"); mu.Unlock(); return nil }, report: func(context.Context, error, ReportInfo) { mu.Lock(); reports++; mu.Unlock() }}); err != nil {
					t.Fatal(err)
				}
				var err error
				if memory {
					err = f.append(f.doc, "kept", math.Inf(1))
				} else {
					_, err = s.Commit(bg, mainDeferredAppend(f.doc, "kept"))
				}
				if err != nil {
					t.Fatal("published call must resolve despite own listener error", err)
				}
				if !errors.Is(s.failure(), first) {
					t.Error("local failure must latch first cause", s.failure())
					return
				}
				end, err := s.Closed(bg)
				if err != nil || end.Reason != "failed" || !errors.Is(end.Error, first) {
					t.Fatal("failed shutdown", end, err)
				}
				mu.Lock()
				got := append([]string(nil), calls...)
				ended := hostEnd
				reported := reports
				mu.Unlock()
				want := []string{"first", "second", "healthy", "host", "end"}
				if memory {
					want = []string{"first", "second", "healthy", "end"}
				}
				if !reflect.DeepEqual(got, want) || ended.Error != first || ended.Reason != "failed" || reported != 0 {
					t.Error("fatal versus guarded errors/order/identity", got, ended, reported)
				}
				reopened := utf16ReopenStore(t, b.store)
				defer reopened.Close(bg)
				state := mainDeferredSnapshot(t, reopened)
				wantText := "kept"
				if memory {
					wantText = ""
				}
				if state.Documents[f.doc].Value["text"] != wantText {
					t.Error("committed store / dropped failed hold", state.Documents[f.doc].Value)
				}
				if _, err = s.Commit(bg, mainDeferredAppend(f.doc, "later")); !errors.Is(err, first) {
					t.Error("later admission lost cause", err)
				}
			})
		})
	}
}

// Source followCommits asserts usability, without acquiring the storage line.
// All writers are released/joined before cleanup even for a blocked old adapter.
func TestMainSessionLocalPublicationMembership(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, add := mainSyncCounterSession(t, b)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		s.taskBookkeeping(func() {
			original := s.core.appendFrame
			s.core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				if original != nil {
					if err := original(kind, ordinal, high, payload); err != nil {
						return err
					}
				}
				if kind == 2 {
					once.Do(func() { close(entered); <-release })
				}
				return nil
			}
		})
		committed := make(chan error, 1)
		go func() { committed <- add() }()
		watchReceive(t, entered)
		var mu sync.Mutex
		calls := 0
		type registration struct {
			detach func()
			err    error
		}
		registered := make(chan registration, 1)
		go func() {
			detach, err := s.followSessionCommits(&sessionCommitListener{callback: func(context.Context, sessionObservedCommit) error { mu.Lock(); calls++; mu.Unlock(); return nil }})
			registered <- registration{detach, err}
		}()
		var attached registration
		select {
		case attached = <-registered:
		case <-time.After(time.Second):
			close(release)
			watchReceive(t, committed)
			attached = watchReceive(t, registered)
			if attached.detach != nil {
				attached.detach()
			}
			t.Fatal("local registration blocked behind storage")
		}
		close(release)
		if err := watchReceive(t, committed); err != nil {
			t.Fatal(err)
		}
		if attached.err != nil {
			t.Fatal(attached.err)
		}
		defer attached.detach()
		mu.Lock()
		count := calls
		mu.Unlock()
		if count != 1 {
			t.Error("publication-time member missed current commit", count)
		}
	})
}

// Native admission/cleanup invariant, not an upstream body. Duplicate Set
// membership consumes no extra slot; native watches/subscriptions/host guards
// and local listeners share the pre-existing bounded process capacity.
func TestMainSessionLocalListenerAdmissionAndClose(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, _ := mainSyncCounterSession(t, b)
		if _, err := s.followSessionCommits(nil); err == nil {
			t.Fatal("nil listener admitted")
		}
		if _, err := s.followSessionCommits(&sessionCommitListener{}); err == nil {
			t.Fatal("nil callback admitted")
		}
		var first *sessionCommitListener
		var firstDetach func()
		for i := 0; i < s.observerCapacity(); i++ {
			listener := &sessionCommitListener{callback: func(context.Context, sessionObservedCommit) error { return nil }}
			detach, err := s.followSessionCommits(listener)
			if err != nil {
				t.Fatal(i, err)
			}
			defer detach()
			if i == 0 {
				first, firstDetach = listener, detach
			}
		}
		duplicate, err := s.followSessionCommits(first)
		if err != nil {
			t.Fatal("duplicate consumed a process slot", err)
		}
		defer duplicate()
		if _, _, err = s.SubscribeCommits(bg); err == nil {
			t.Fatal("legacy subscription exceeded shared capacity")
		}
		if _, err = s.observeStoredCommits(sessionCommitObserver{advance: func(context.Context, CommitPublication) error { return nil }, end: func(WatchEnd) error { return nil }}); err == nil {
			t.Fatal("stored observer exceeded shared capacity")
		}
		firstDetach()
		firstDetach()
		again, err := s.followSessionCommits(first)
		if err != nil {
			t.Fatal("detached slot not reusable", err)
		}
		defer again()
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
		s.observerMu.Lock()
		retained := len(s.localCommitListeners)
		s.observerMu.Unlock()
		if retained != 0 {
			t.Error("Close retained local callbacks", retained)
		}
		if _, err = s.followSessionCommits(first); !errors.Is(err, ErrClosed) {
			t.Error("closed Session admitted listener", err)
		}
	})
}
