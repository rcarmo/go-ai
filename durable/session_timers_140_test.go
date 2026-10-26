package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func documentTimerFixture140(t *testing.T, s *Session, calls *atomic.Int64) *DocumentDefinition {
	t.Helper()
	def, err := DefineDocument(DefinitionOptions{Kind: "timer-cache", Version: 2, Scope: "session", CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }, Initial: func(JSON) (JSON, error) { return JSON{"x": 0}, nil }, Migrate: func(value JSON, _ uint64) (JSON, error) { calls.Add(1); return value, nil }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Commit(bg, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		_, err = tx.CreateDocument(Document{ID: id, Kind: "timer-cache", Version: 1, Scope: "session", Value: JSON{"x": 1}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return def
}
func TestSessionTimer140BackgroundDeclineAndFailureSweepOnCommit(t *testing.T) {
	for _, panics := range []bool{false, true} {
		backends(t, func(t *testing.T, b backend) {
			var clock, calls, arms, reports atomic.Int64
			clock.Store(1000)
			retention := int64(20)
			var current atomic.Pointer[Session]
			s, err := OpenSessionWithOptions(b.store, SessionOptions{Now: clock.Load, DocumentRetentionMs: &retention, SetTimer: func(_ func(), _ time.Duration, mode TimerOptions) func() {
				arms.Add(1)
				if !mode.Background {
					t.Error("cache requested foreground timer")
				}
				if _, err := current.Load().Snapshot(bg); err != nil {
					t.Error("host timer reentrancy", err)
				}
				if panics {
					panic("host has no timer")
				}
				return nil
			}, OnReport: func(error) {
				reports.Add(1)
				if _, err := current.Load().Snapshot(bg); err != nil {
					t.Error("report reentrancy", err)
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			current.Store(s)
			defer s.Close(bg)
			def := documentTimerFixture140(t, s, &calls)
			for range 2 {
				if _, ok, err := s.SnapshotDefinition(bg, def, 0, nil); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			if calls.Load() != 1 || arms.Load() == 0 {
				t.Fatal("warm cache or background hook", calls.Load(), arms.Load())
			}
			clock.Store(1020)
			// There is deliberately no host timer. A genuine unrelated commit must
			// perform opportunistic eviction without touching the retained definition.
			if _, err = s.Commit(bg, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				_, err = tx.CreateDocument(Document{ID: id, Kind: "unrelated", Version: 1, Scope: "session", Value: JSON{}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			s.taskBookkeeping(func() {
				if len(s.definitionCache) != 0 {
					t.Error("declined timer prevented commit-time eviction")
				}
			})
			if _, ok, err := s.SnapshotDefinition(bg, def, 0, nil); err != nil || !ok {
				t.Fatal(ok, err)
			}
			if calls.Load() != 2 {
				t.Fatal("expired value did not migrate again", calls.Load())
			}
			if panics && reports.Load() == 0 || !panics && reports.Load() != 0 {
				t.Fatal("optional decline/panic reporting", reports.Load())
			}
		})
	}
}
func TestSessionTimer140DocumentEarlyFireAndClose(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock, calls atomic.Int64
		clock.Store(1000)
		retention := int64(20)
		armed := make(chan *capturedTimer140, 8)
		var current atomic.Pointer[Session]
		s, err := OpenSessionWithOptions(b.store, SessionOptions{Now: clock.Load, DocumentRetentionMs: &retention, SetTimer: func(callback func(), _ time.Duration, mode TimerOptions) func() {
			if !mode.Background {
				t.Error("cache timer not optional")
			}
			if _, err := current.Load().Snapshot(bg); err != nil {
				t.Error(err)
			}
			node := &capturedTimer140{fire: callback}
			armed <- node
			return func() {
				node.cancelled.Add(1)
				if _, err := current.Load().Snapshot(bg); err != nil && err != ErrClosed {
					t.Error(err)
				}
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		current.Store(s)
		defer s.Close(bg)
		next := func() *capturedTimer140 {
			t.Helper()
			select {
			case node := <-armed:
				return node
			case <-time.After(3 * time.Second):
				t.Fatal("document expiry timer missing")
				return nil
			}
		}
		def := documentTimerFixture140(t, s, &calls)
		if _, _, err = s.SnapshotDefinition(bg, def, 0, nil); err != nil {
			t.Fatal(err)
		}
		first := next()
		clock.Store(1019)
		first.fire()
		second := next()
		s.taskBookkeeping(func() {
			if len(s.definitionCache) != 1 {
				t.Error("early timer evicted cache")
			}
		})
		if first.cancelled.Load() != 1 {
			t.Fatal("early expired timer not disarmed")
		}
		clock.Store(1020)
		second.fire()
		s.taskBookkeeping(func() {
			if len(s.definitionCache) != 0 {
				t.Error("due timer did not evict")
			}
		})
		if _, _, err = s.SnapshotDefinition(bg, def, 0, nil); err != nil {
			t.Fatal(err)
		}
		third := next()
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
		if third.cancelled.Load() != 1 {
			t.Fatal("Close did not disarm current cache timer")
		}
		third.fire()
		if calls.Load() != 2 {
			t.Fatal("late timer reused closed cache")
		}
	})
}
func TestSessionTimer140CloseJoinsInFlightArm(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var cancelled, calls atomic.Int64
		s, err := OpenSessionWithOptions(b.store, SessionOptions{SetTimer: func(func(), time.Duration, TimerOptions) func() {
			close(entered)
			<-release
			return func() { cancelled.Add(1) }
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		defer releaseTaskGate(release)
		def := documentTimerFixture140(t, s, &calls)
		read := make(chan error, 1)
		go func() { _, _, err := s.SnapshotDefinition(bg, def, 0, nil); read <- err }()
		awaitTaskSignal(t, entered)
		closed := make(chan error, 1)
		go func() { closed <- s.Close(bg) }()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for !s.closing.Load() {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
		select {
		case <-closed:
			t.Fatal("Close returned before admitted host arm")
		default:
		}
		releaseTaskGate(release)
		select {
		case err := <-read:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case err := <-closed:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if cancelled.Load() != 1 {
			t.Fatal("in-flight host arm not cancelled exactly once", cancelled.Load())
		}
	})
}
func TestSessionTimer140HarnessContextExpiryUsesHost(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		root := mint(t, b.store)
		apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: root}})
		var clock atomic.Int64
		clock.Store(1000)
		armed := make(chan *capturedTimer140, 8)
		h := taskTestHarnessOptions(t, b.store, Options{Now: clock.Load, SetTimer: func(callback func(), delay time.Duration, mode TimerOptions) func() {
			if !mode.Background || delay != 10*time.Minute {
				t.Errorf("context expiry mode/delay %+v %v", mode, delay)
			}
			node := &capturedTimer140{fire: callback}
			armed <- node
			return func() { node.cancelled.Add(1) }
		}})
		id, err := h.session.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) error { return tx.AppendEntry(contextEntry(id, root, goai.RoleUser, "context")) }); err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.ContextView(bg, root, 0); err != nil {
			t.Fatal(err)
		}
		var node *capturedTimer140
		select {
		case node = <-armed:
		case <-time.After(3 * time.Second):
			t.Fatal("context expiry not routed through Harness.SetTimer")
		}
		clock.Store(601000)
		node.fire()
		h.session.taskBookkeeping(func() {
			if len(h.session.contextRanges) != 0 {
				t.Error("host timer did not evict idle context")
			}
		})
		if node.cancelled.Load() != 1 {
			t.Fatal("context timer not disarmed")
		}
	})
}

func TestSessionTimer140CoalescesOnLineReschedules(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var arms, cancels, reports, callbacks atomic.Int64
		s, err := OpenSessionWithOptions(b.store, SessionOptions{SetTimer: func(func(), time.Duration, TimerOptions) func() {
			arms.Add(1)
			return func() { cancels.Add(1); panic("cancel failed") }
		}, OnReport: func(error) { reports.Add(1) }})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		s.taskBookkeeping(func() {
			for range 10000 {
				if s.contextExpiry != nil {
					s.contextExpiry.Stop()
				}
				s.contextExpiry = s.newSessionTimer(contextExpiryTimer, time.Hour, func() { callbacks.Add(1) })
				if s.documentExpiry != nil {
					s.documentExpiry.Stop()
				}
				s.documentExpiry = s.newSessionTimer(documentExpiryTimer, time.Hour, func() { callbacks.Add(1) })
			}
		})
		if arms.Load() != 2 {
			t.Fatal("unbounded pending timer arms", arms.Load())
		}
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
		if cancels.Load() != 2 || reports.Load() != 2 || callbacks.Load() != 0 {
			t.Fatal(cancels.Load(), reports.Load(), callbacks.Load())
		}
	})
}
