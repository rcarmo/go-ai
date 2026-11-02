package durable

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// c9f4c7b9 session-deferred-commits.test.ts:347. Native adaptations: the
// reserved root is the first conversation; sequence/delta counts and queued
// stored publications witness real batches; Snapshot is the on-line barrier.
func TestMainSessionDeferredDeadlineStoresEveryHold(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		var other, otherDoc ID
		if _, err := f.session.Commit(bg, func(tx *Tx) error {
			var err error
			other, err = tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.CreateConversation(Conversation{ID: other}); err != nil {
				return err
			}
			otherDoc, err = tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(mainDeadlineDocument(otherDoc, other))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := f.append(f.doc, "one"); err != nil {
			t.Fatal(err)
		}
		if err := f.append(otherDoc, "two"); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 1000*time.Millisecond)
		before := mainDeferredSnapshot(t, b.store)
		publications := f.publications()
		f.fire(t)
		after := mainDeferredSnapshot(t, b.store)
		if after.Seq != before.Seq+1 {
			t.Fatal("deadline batch count", before.Seq, after.Seq)
		}
		visible, err := f.session.Snapshot(bg)
		if err != nil || visible.Seq != after.Seq || !equalJSONValue(visible.Documents[otherDoc].Value, after.Documents[otherDoc].Value) {
			t.Fatal("deadline did not adopt receipted image", visible.Seq, after.Seq, err)
		}
		if after.Documents[f.doc].DeltasSinceBase != 1 || after.Documents[otherDoc].DeltasSinceBase != 1 {
			t.Fatal("stored change count", after.Documents)
		}
		if f.publications() != publications+1 {
			t.Fatal("deadline stored publication count")
		}
		f.session.taskBookkeeping(func() {
			if len(f.session.heldDocuments) != 0 {
				t.Error("deadline kept holds")
			}
		})
		f.assertArmed(t)
		if !equalJSONValue(after.Documents[otherDoc].Value, JSON{"text": "two"}) {
			t.Fatal("other stored value", after.Documents[otherDoc].Value)
		}
	})
}

// c9f4c7b9 session-deferred-commits.test.ts:365, preserving 2000, +500,
// explicit 100, and later 60000. An earlier deadline moves; a later one cannot.
func TestMainSessionDeferredDeadlineEarliestWindow(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		window := float64(2000)
		f := openMainDeadlineSession(t, b, &window)
		if err := f.append(f.doc, "a"); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 2000*time.Millisecond)
		f.clock.Add(500)
		if err := f.append(f.doc, "b", 100); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 100*time.Millisecond)
		if err := f.append(f.doc, "c", 60000); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 100*time.Millisecond)
	})
}

// c9f4c7b9 session-deferred-commits.test.ts:378. Infinity creates no timer;
// a later default window creates one; an immediate dependent write clears it.
func TestMainSessionDeferredDeadlineInfinityAndRelease(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		if _, err := f.session.commitDeferred(bg, mainDeferredAppend(f.doc, "a")); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t)
		f.session.taskBookkeeping(func() {
			if len(f.session.heldDocuments) != 1 {
				t.Error("infinite window did not hold")
			}
		})
		if err := f.append(f.doc, "b"); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 1000*time.Millisecond)
		if _, err := f.session.Commit(bg, mainDeferredAppend(f.doc, "c")); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t)
		f.session.taskBookkeeping(func() {
			if len(f.session.heldDocuments) != 0 {
				t.Error("immediate write kept hold")
			}
		})
	})
}

// c9f4c7b9 session-deferred-commits.test.ts:273. Source TypeError maps to
// native StorageRejected; preserve both numeric inputs and immediate control.
func TestMainSessionDeferredDeadlineInvalidWindow(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		for _, window := range []float64{-1, math.NaN()} {
			var rejected *StorageRejected
			if err := f.append(f.doc, "a", window); !errors.As(err, &rejected) {
				t.Fatalf("invalid window %v: %v", window, err)
			}
		}
		if _, err := f.session.Commit(bg, mainDeferredAppend(f.doc, "a")); err != nil {
			t.Fatal(err)
		}
	})
}

// Native host/serialization controls: callbacks enqueue without waiting for an
// admitted commit, stale cancelled hosts cannot store, and repeated fires store once.
func TestMainSessionDeferredDeadlineHostAdmission(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		if err := f.append(f.doc, "a", 2000); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 2000*time.Millisecond)
		old := f.active()[0]
		if err := f.append(f.doc, "b", 100); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, 100*time.Millisecond)
		before := mainDeferredSnapshot(t, b.store).Seq
		old.fire()
		if _, err := f.session.Snapshot(bg); err != nil {
			t.Fatal(err)
		}
		if mainDeferredSnapshot(t, b.store).Seq != before {
			t.Fatal("stale timer stored a hold")
		}
		entered, release, committed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		var once sync.Once
		defer once.Do(func() { close(release) })
		go func() {
			_, err := f.session.Commit(bg, func(*Tx) error { close(entered); <-release; return nil })
			committed <- err
		}()
		awaitTaskSignal(t, entered)
		current := f.active()[0]
		returned := make(chan struct{})
		go func() { current.fire(); current.fire(); close(returned) }()
		awaitTaskSignal(t, returned)
		once.Do(func() { close(release) })
		if err := watchReceive(t, committed); err != nil {
			t.Fatal(err)
		}
		if _, err := f.session.Snapshot(bg); err != nil {
			t.Fatal(err)
		}
		if mainDeferredSnapshot(t, b.store).Seq != before+1 {
			t.Fatal("fired timer did not store exactly once")
		}
	})
}

func TestMainSessionDeferredDeadlineFailure(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		f := openMainDeadlineSession(t, b, nil)
		if err := f.append(f.doc, "volatile"); err != nil {
			t.Fatal(err)
		}
		f.assertArmed(t, time.Second)
		refusal := &StorageRejected{Reason: "deadline flush refused"}
		var attempts atomic.Int64
		f.session.taskBookkeeping(func() {
			core := f.session.core
			if err := core.enter(bg); err != nil {
				t.Fatal(err)
			}
			defer core.leave()
			original := core.appendFrame
			core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				if kind == 2 {
					attempts.Add(1)
					return refusal
				}
				if original != nil {
					return original(kind, ordinal, high, payload)
				}
				return nil
			}
		})
		f.active()[0].fire()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		end, err := f.session.Closed(ctx)
		if err != nil || end.Reason != "failed" || !errors.Is(end.Error, refusal) {
			t.Fatal("deadline failure/closure", end, err)
		}
		if attempts.Load() != 1 {
			t.Fatal("failure retried volatile holds", attempts.Load())
		}
		reopened := utf16ReopenStore(t, b.store)
		defer reopened.Close(bg)
		if value := mainDeferredSnapshot(t, reopened).Documents[f.doc].Value; !equalJSONValue(value, JSON{"text": ""}) {
			t.Fatal("refused deadline became durable", value)
		}
	})
}

type mainDeadlineTimer struct {
	capturedTimer140
	delay time.Duration
	fired atomic.Bool
}

type mainDeadlineSession struct {
	session       *Session
	doc           ID
	clock         atomic.Int64
	mu            sync.Mutex
	timers        []*mainDeadlineTimer
	stored        *CommitSubscription
	defaultWindow *float64
}

func openMainDeadlineSession(t *testing.T, b backend, defaultWindow *float64) *mainDeadlineSession {
	t.Helper()
	f := &mainDeadlineSession{defaultWindow: defaultWindow}
	f.clock.Store(1000)
	s, err := OpenSessionWithOptions(b.store, SessionOptions{Now: f.clock.Load, SetTimer: func(callback func(), delay time.Duration, mode TimerOptions) func() {
		if mode.Background {
			return nil
		}
		if _, err := f.session.Snapshot(bg); err != nil {
			t.Error("foreground arm reentrancy", err)
		}
		node := &mainDeadlineTimer{capturedTimer140: capturedTimer140{fire: callback}, delay: delay}
		f.mu.Lock()
		f.timers = append(f.timers, node)
		f.mu.Unlock()
		return func() {
			node.cancelled.Add(1)
			if _, err := f.session.Snapshot(bg); err != nil && !errors.Is(err, ErrClosed) && f.session.failure() == nil {
				t.Error("foreground cancel reentrancy", err)
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.session = s
	if f.defaultWindow != nil {
		s.persistWithinMs = func() float64 { return *f.defaultWindow }
	}
	t.Cleanup(func() {
		if err := s.Close(bg); err != nil {
			t.Error(err)
		}
	})
	if _, err = s.Commit(bg, func(tx *Tx) error {
		var err error
		f.doc, err = tx.MintID()
		if err != nil {
			return err
		}
		_, err = tx.CreateDocument(mainDeadlineDocument(f.doc, 1))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, f.stored, err = s.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.stored.Stop() })
	return f
}
func mainDeadlineDocument(id, owner ID) Document {
	return Document{ID: id, Owner: owner, Scope: "conversation", Kind: "test.deferred.text", Version: 1, History: "latest", Fork: "initial", Value: JSON{"text": ""}}
}
func (f *mainDeadlineSession) append(id ID, text string, withinMs ...float64) error {
	// Internal native option boundary; public/Harness wiring is still gated.
	_, err := f.session.commitPersistence(bg, mainDeferredAppend(id, text), true, 0, true, withinMs...)
	return err
}
func (f *mainDeadlineSession) active() []*mainDeadlineTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []*mainDeadlineTimer
	for _, timer := range f.timers {
		if timer.cancelled.Load() == 0 && !timer.fired.Load() {
			result = append(result, timer)
		}
	}
	return result
}
func (f *mainDeadlineSession) assertArmed(t *testing.T, delays ...time.Duration) {
	t.Helper()
	armed := f.active()
	if len(armed) != len(delays) {
		t.Fatalf("foreground persistence timers: got %d, want delays %v", len(armed), delays)
	}
	for i, timer := range armed {
		if timer.delay != delays[i] {
			t.Fatalf("timer delay %s, want %s", timer.delay, delays[i])
		}
	}
}
func (f *mainDeadlineSession) publications() int {
	f.stored.mu.Lock()
	defer f.stored.mu.Unlock()
	return len(f.stored.pending)
}
func (f *mainDeadlineSession) fire(t *testing.T) {
	t.Helper()
	for _, timer := range f.active() {
		if timer.fired.CompareAndSwap(false, true) {
			timer.fire()
		}
	}
	// Source fire() awaits readOnLine. Native Snapshot owns the same line.
	if _, err := f.session.Snapshot(bg); err != nil {
		t.Fatal(err)
	}
}
